// MIT License
//
// (C) Copyright [2020-2021] Hewlett Packard Enterprise Development LP
//
// Permission is hereby granted, free of charge, to any person obtaining a
// copy of this software and associated documentation files (the "Software"),
// to deal in the Software without restriction, including without limitation
// the rights to use, copy, modify, merge, publish, distribute, sublicense,
// and/or sell copies of the Software, and to permit persons to whom the
// Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included
// in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
// THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR
// OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
// ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.

package service_reservations

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/sirupsen/logrus"
)

const HSM_DEFAULT_RESERVATION_PATH = "/hsm/v2/locks/service/reservations"
const HSM_DEFAULT_SERVER = "https://api-gw-service-nmn/apis/smd"
const DEFAULT_TERM_MINUTES = 1
const DefaultExpirationWindow = 30
const DefaultSleepTime = 10

//TODO - Future Enhancements
// * ALLOW both rigid an flexible processing -> not sure rigid release makes sense... so really just flexible on the AQUIRE
// * alter CHECK, RELEASE functions to cope with flexibility -> return an array of tuples: something like xname, bool, error, etc
// * consider if the aquire and release should be idempotent, such that a second call right away doesnt error; or returns a special error

//TODO - Today
// Sample application

// Implementation considerations
// Should I periodically call CloseIdleConnections? (perhaps in the renewal function)  So far dont see the need
// Should I pass a reference to the http client?
// Should I return the actual structures? At this point I dont think there is a need.
// Should I provide a way to shutdown the renewal thread? This will be either used in a service FOREVER or in a simple app; so not needed imho.

type Reservation struct {
	Xname          string
	Expiration     time.Time
	DeputyKey      string
	ReservationKey string
}

type Production struct {
	httpClient         *retryablehttp.Client
	stateManagerServer string
	reservationPath    string
	defaultTermMinutes int
	reservedMap        map[string]Reservation
	reservationMutex   sync.Mutex
	configured         bool
	logger             *logrus.Logger
}

//This uses the rigid implementation, so we will assume for EVERY operation that is all or nothing.  This could be
//pretty easily extended to support the flexible implementation, but this wasnt needed for FAS/CAPMC
type ServiceReservation interface {
	Init(stateManagerServer string, reservationPath string, defaultTermMinutes int, logger *logrus.Logger)

	//Try to aquire the lock, renewing it within 30 seconds of expiration.
	Aquire(xnames []string) error

	//Validate that I still own the lock for the xnames listed.
	Check(xnames []string) bool

	//Release the locks on the xnames
	Release(xnames []string) error

	Status() map[string]Reservation
}

func (i *Production) Init(stateManagerServer string, reservationPath string, defaultTermMinutes int, logger *logrus.Logger) {

	if i.configured == false { // ONE TIME ONLY!
		i.configured = true

		if logger != nil {
			i.logger = logger
		} else {
			i.logger = logrus.New()
		}

		//setup clients and defaults

		i.httpClient = retryablehttp.NewClient()
		i.httpClient.HTTPClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		i.httpClient.RetryMax = 5
		i.httpClient.HTTPClient.Timeout = time.Second * 40
		//turn off the http client loggin!
		tmpLogger := logrus.New()
		tmpLogger.SetLevel(logrus.PanicLevel)
		i.httpClient.Logger = tmpLogger

		if len(stateManagerServer) == 0 {
			i.stateManagerServer = HSM_DEFAULT_SERVER
		} else {
			i.stateManagerServer = stateManagerServer
		}

		if len(reservationPath) == 0 {
			i.reservationPath = HSM_DEFAULT_RESERVATION_PATH
		} else {
			i.reservationPath = reservationPath
		}

		if defaultTermMinutes < 1 || defaultTermMinutes > 15 {
			i.defaultTermMinutes = DEFAULT_TERM_MINUTES
		} else {
			i.defaultTermMinutes = defaultTermMinutes
		}

		i.reservationMutex = sync.Mutex{}

		i.reservedMap = make(map[string]Reservation)

		go i.doRenewal()
	}
}

//Lets make this really simple; Im going to wake up and see what expires in the next 30 seconds;
//then I will renew those things
func (i *Production) doRenewal() {

	for ; ; time.Sleep(time.Duration(DefaultSleepTime) * time.Second) {
		i.logger.Trace("doRenewal() - TOP Loop")

		i.update() // once per loop call the update function to see what is still viable!

		if len(i.reservedMap) == 0 {
			i.logger.Debug("doRenewal() - CONTINUE - empty map, nothing to do")
			continue
		}

		var renewalCandidates []string
		for k, v := range i.reservedMap {
			if v.Expiration.Before(time.Now()) { //expired -> no point in renewing!
				i.logger.WithFields(logrus.Fields{"reservation": v, "expiration time": v.Expiration, "clock time": time.Now()}).Trace("doRenewal() - CONTINUE - Deleting from map @1")

				i.reservationMutex.Lock()
				delete(i.reservedMap, k)
				i.reservationMutex.Unlock()

				continue
			} else if v.Expiration.Add(DefaultExpirationWindow * -1 * time.Second).Before(time.Now()) {
				// is it within the 30 second expiration window?
				renewalCandidates = append(renewalCandidates, k)
			}
		}

		var renewalParameters ReservationRenewalParameters
		renewalParameters.ProcessingModel = "flexible"
		renewalParameters.ReservationDuration = i.defaultTermMinutes

		for _, xname := range renewalCandidates {
			if res, ok := i.reservedMap[xname]; ok {

				key := Key{
					ID:  res.Xname,
					Key: res.ReservationKey,
				}

				//add the key to the list
				renewalParameters.ReservationKeys = append(renewalParameters.ReservationKeys, key)
			}
		}

		if len(renewalCandidates) == 0 {
			i.logger.Debug("doRenewal() - CONTINUE - Nothing to renew")
			continue
		}
		marshalReleaseParams, _ := json.Marshal(renewalParameters)
		stringReleaseParams := string(marshalReleaseParams)
		targetURL, _ := url.Parse(i.stateManagerServer + i.reservationPath + "/renew")

		i.logger.WithField("params", stringReleaseParams).Trace("doRenewal() - Sending command")

		newRequest, err := http.NewRequest("POST", targetURL.String(), bytes.NewBuffer([]byte(stringReleaseParams)))
		if err != nil {
			i.logger.WithField("error", err).Error("doRenewal() - CONTINUE")
			continue //go to sleep
		}

		reqContext, _ := context.WithTimeout(context.Background(), time.Second*40)
		req, err := retryablehttp.FromRequest(newRequest)
		req = req.WithContext(reqContext)
		if err != nil {
			i.logger.WithField("error", err).Error("doRenewal() - CONTINUE")
			continue //go to sleep
		}

		req.Header.Add("Content-Type", "application/json")

		//make request
		resp, err := i.httpClient.Do(req)
		if err != nil {
			i.logger.WithField("error", err).Error("doRenewal() - CONTINUE")
			continue //go to sleep
		}

		//process response
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			i.logger.WithField("error", err).Error("doRenewal() - CONTINUE")
			continue //go to sleep
		}

		i.logger.WithField("response", string(body)).Trace("doRenewal() - Received response")

		switch statusCode := resp.StatusCode; statusCode {

		case http.StatusOK:
			var response ReservationReleaseRenewResponse
			err = json.Unmarshal(body, &response)
			if err != nil {
				i.logger.WithField("error", err).Error("doRenewal() - CONTINUE")
				continue
			}

			i.logger.WithFields(logrus.Fields{"Total": response.Counts.Total,
				"Success": response.Counts.Success,
				"Failure": response.Counts.Failure}).Debug("doRenewal() - renewal action complete")
			i.logger.WithFields(logrus.Fields{"Total": response.Counts.Total,
				"Success": response.Counts.Success,
				"Failure": response.Counts.Failure}).Info("ServiceReservations - renewal action complete")

			// Remove the failed ones
			for _, v := range response.Failure {
				if _, ok := i.reservedMap[v.ID]; ok {
					i.logger.WithFields(logrus.Fields{"reservation": i.reservedMap[v.ID]}).Trace("doRenewal() - deleting failures")
					i.reservationMutex.Lock()
					delete(i.reservedMap, v.ID)
					i.reservationMutex.Unlock()
				}
			}

		//Die silently?
		default:
			i.logger.WithFields(logrus.Fields{"error": "failed to renew", "renewalCandidates": renewalCandidates}).Error("doRenewal() - CONTINUE")
			continue
		}
		i.logger.Trace("doRenewal() - BOTTOM Loop")
	}
}

func (i *Production) Aquire(xnames []string) error {
	i.logger.Trace("Aquire() - START")

	//prepare the request
	reservation := ReservationCreateParameters{
		ID:                  xnames,
		ProcessingModel:     CLProcessingModelRigid,
		ReservationDuration: i.defaultTermMinutes,
	}
	marshalReservation, _ := json.Marshal(reservation)
	stringReservation := string(marshalReservation)
	targetURL, _ := url.Parse(i.stateManagerServer + i.reservationPath)
	i.logger.WithField("params", stringReservation).Trace("Aquire() - Sending command")
	newRequest, err := http.NewRequest("POST", targetURL.String(), bytes.NewBuffer([]byte(stringReservation)))
	if err != nil {
		i.logger.WithField("error", err).Error("Aquire() - END")
		return err
	}

	reqContext, _ := context.WithTimeout(context.Background(), time.Second*40)
	req, err := retryablehttp.FromRequest(newRequest)
	req = req.WithContext(reqContext)
	if err != nil {
		i.logger.WithField("error", err).Error("Aquire() - END")
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	//make request
	resp, err := i.httpClient.Do(req)
	if err != nil {
		i.logger.WithField("error", err).Error("Aquire() - END")
		return err
	}

	//process response
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		i.logger.WithField("error", err).Error("Aquire() - END")
		return err
	}

	i.logger.WithField("response", string(body)).Trace("Aquire() - Recieved response")

	switch statusCode := resp.StatusCode; statusCode {

	case http.StatusOK:
		var response ReservationCreateResponse
		err = json.Unmarshal(body, &response)
		if err != nil {
			i.logger.WithField("error", err).Error("Aquire() - END")
			return err
		}

		if len(response.Failure) > 0 {
			// because we have HARDCODED rigid; if failure > 0; success MUST == 0
			err = errors.New("at least one xname could not be reserved")
			i.logger.WithField("error", err).Error("Aquire() - END")
			return err
		}

		//else load the maps
		for _, v := range response.Success {
			res := Reservation{
				Xname:          v.ID,
				DeputyKey:      v.DeputyKey,
				ReservationKey: v.ReservationKey,
			}
			res.Expiration, _ = time.Parse(time.RFC3339, v.ExpirationTime)
			i.reservationMutex.Lock()
			i.reservedMap[res.Xname] = res
			i.reservationMutex.Unlock()
		}
		i.logger.Trace("Aquire() - END")
		return nil

	case http.StatusBadRequest:
		var response Problem7807
		_ = json.Unmarshal(body, &response)
		err = errors.New(response.Detail)
		i.logger.WithField("error", err).Error("Aquire() - END")
		return err

	default:
		err = errors.New(string(body))
		i.logger.WithField("error", err).Error("Aquire() - END")
		return err
	}

}

func (i *Production) Check(xnames []string) bool {
	i.logger.Trace("Check() - START")

	valid := true
	for _, xname := range xnames {
		if _, ok := i.reservedMap[xname]; !ok {
			valid = false
			break
		}
	}
	i.logger.Trace("Check() - END")
	return valid
}

func (i *Production) Release(xnames []string) error {

	i.logger.Trace("Release() - START")
	if len(xnames) == 0 {
		i.logger.Trace("release() - END -> nothing to do ")
		err := errors.New("empty set; failing release operation")
		return err
	}

	var releaseParams ReservationReleaseParameters
	releaseParams.ProcessingModel = "flexible" //TODO should this be rigid?

	for _, xname := range xnames {
		if res, ok := i.reservedMap[xname]; ok {

			key := Key{
				ID:  res.Xname,
				Key: res.ReservationKey,
			}

			//add the key to the list
			releaseParams.ReservationKeys = append(releaseParams.ReservationKeys, key)

		} else { // xname not in map
			err := errors.New(xname + " not present; failing release operation")
			i.logger.WithField("error", err).Error("Release() - END")
			return err
		}
	}

	marshalReleaseParams, _ := json.Marshal(releaseParams)
	stringReleaseParams := string(marshalReleaseParams)
	targetURL, _ := url.Parse(i.stateManagerServer + i.reservationPath + "/release")

	i.logger.WithField("Params:", stringReleaseParams).Trace("Release() - Sending command")

	newRequest, err := http.NewRequest("POST", targetURL.String(), bytes.NewBuffer([]byte(stringReleaseParams)))
	if err != nil {
		i.logger.WithField("error", err).Error("Release() - END")
		return err
	}

	reqContext, _ := context.WithTimeout(context.Background(), time.Second*40)
	req, err := retryablehttp.FromRequest(newRequest)
	req = req.WithContext(reqContext)
	if err != nil {
		i.logger.WithField("error", err).Error("Release() - END")
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	//make request
	resp, err := i.httpClient.Do(req)
	if err != nil {
		i.logger.WithField("error", err).Error("Release() - END")
		return err
	}

	//process response
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		i.logger.WithField("error", err).Error("Release() - END")
		return err
	}
	i.logger.WithField("response", string(body)).Trace("Release() - Received response")

	switch statusCode := resp.StatusCode; statusCode {

	case http.StatusOK:
		var response ReservationReleaseRenewResponse
		err = json.Unmarshal(body, &response)
		if err != nil {
			i.logger.WithField("error", err).Error("Release() - END")
			return err
		}

		i.logger.WithFields(logrus.Fields{"Total": response.Counts.Total,
			"Success": response.Counts.Success,
			"Failure": response.Counts.Failure}).Debug("Release() - release action complete")

		for _, xname := range response.Success.ComponentIDs {
			if _, ok := i.reservedMap[xname]; ok {
				i.logger.WithFields(logrus.Fields{"reservation": i.reservedMap[xname]}).Trace("Release() - deleting released reservations")

				i.reservationMutex.Lock()
				delete(i.reservedMap, xname)
				i.reservationMutex.Unlock()
			}
		}

		if len(response.Failure) > 0 {
			err = errors.New("at least one xname could not be released")
			i.logger.WithField("error", err).Error("Release() - END")
			return err
		}

		return nil

	case http.StatusBadRequest:
		var response Problem7807
		_ = json.Unmarshal(body, &response)
		err = errors.New(response.Detail)
		i.logger.WithField("error", err).Error("Release() - END")
		return err

	default:
		err = errors.New(string(body))
		i.logger.WithField("error", err).Error("Release() - END")
		return err
	}

}

func (i *Production) update() {
	i.logger.Trace("update() - START")
	var checkParameters ReservationCheckParameters

	for _, res := range i.reservedMap {
		key := Key{
			ID:  res.Xname,
			Key: res.DeputyKey,
		}
		//add the key to the list
		checkParameters.DeputyKeys = append(checkParameters.DeputyKeys, key)
	}

	if len(checkParameters.DeputyKeys) == 0 {
		i.logger.Trace("update() - END -> nothing to do ")
		return
	}
	marshalParams, _ := json.Marshal(checkParameters)
	stringParams := string(marshalParams)
	targetURL, _ := url.Parse(i.stateManagerServer + i.reservationPath + "/check")
	i.logger.WithField("params", stringParams).Trace("update() - Sending comand")

	newRequest, err := http.NewRequest("POST", targetURL.String(), bytes.NewBuffer([]byte(stringParams)))
	if err != nil {
		//die silently
		i.logger.WithField("error", err).Error("update() - END")
		return
	}

	reqContext, _ := context.WithTimeout(context.Background(), time.Second*40)
	req, err := retryablehttp.FromRequest(newRequest)
	req = req.WithContext(reqContext)
	if err != nil {
		i.logger.WithField("error", err).Error("update() - END")
		return
	}

	req.Header.Add("Content-Type", "application/json")

	//make request
	resp, err := i.httpClient.Do(req)
	if err != nil {
		i.logger.WithField("error", err).Error("update() - END")
		return
	}

	//process response
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		i.logger.WithField("error", err).Error("update() - END")
		return
	}

	i.logger.WithField("response", string(body)).Trace("update() - Received response")

	switch statusCode := resp.StatusCode; statusCode {

	case http.StatusOK:
		var response ReservationCheckResponse
		err = json.Unmarshal(body, &response)
		if err != nil {
			i.logger.WithField("error", err).Error("update() - END")
			return
		}

		//i.logger.WithFields(logrus.Fields{"failure count:":len(response.Failure),
		//"success count:":len(response.Success), "failure":response.Failure, "success":response.Success}).Trace("StatusOK")

		for _, v := range response.Failure {
			i.logger.WithFields(logrus.Fields{"reservation": i.reservedMap[v.ID]}).Trace("deleting: removing failures in update()")

			i.reservationMutex.Lock()
			delete(i.reservedMap, v.ID)
			i.reservationMutex.Unlock()
		}

		for _, v := range response.Success {
			if _, ok := i.reservedMap[v.ID]; ok {
				i.reservationMutex.Lock()
				tmp := i.reservedMap[v.ID]
				tmp.Expiration, _ = time.Parse(time.RFC3339, v.ExpirationTime)
				i.reservedMap[v.ID] = tmp
				i.reservationMutex.Unlock()
			} else {
				i.logger.WithFields(logrus.Fields{"reservation": i.reservedMap[v.ID]}).Trace("deleting: removing not found in update()")
				i.reservationMutex.Lock()
				delete(i.reservedMap, v.ID)
				i.reservationMutex.Unlock()
			}
		}
		return
	default:
		i.logger.Warn("failed check from update()")
		return
	}
}

func (i *Production) Status() (res map[string]Reservation) {
	i.logger.Trace("Status() - START")
	res = make(map[string]Reservation)
	for k, v := range i.reservedMap {
		res[k] = v
	}
	i.logger.Trace("Status() - END")
	return res
}
