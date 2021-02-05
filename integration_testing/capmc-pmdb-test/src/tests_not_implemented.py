#!/usr/bin/env python3

#  MIT License
#
#  (C) Copyright [2019-2021] Hewlett Packard Enterprise Development LP
#
#  Permission is hereby granted, free of charge, to any person obtaining a
#  copy of this software and associated documentation files (the "Software"),
#  to deal in the Software without restriction, including without limitation
#  the rights to use, copy, modify, merge, publish, distribute, sublicense,
#  and/or sell copies of the Software, and to permit persons to whom the
#  Software is furnished to do so, subject to the following conditions:
#
#  The above copyright notice and this permission notice shall be included
#  in all copies or substantial portions of the Software.
#
#  THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
#  IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
#  FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
#  THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR
#  OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
#  ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
#  OTHER DEALINGS IN THE SOFTWARE.

#  MIT License
#
#
#  Permission is hereby granted, free of charge, to any person obtaining a
#  copy of this software and associated documentation files (the "Software"),
#  to deal in the Software without restriction, including without limitation
#  the rights to use, copy, modify, merge, publish, distribute, sublicense,
#  and/or sell copies of the Software, and to permit persons to whom the
#  Software is furnished to do so, subject to the following conditions:
#
#
from src.config import get_capmc_url
from src.tests_base import register_test, execute_request

# run the tests of endpoints that should return not implemented
def register_tests():
    register_test(test_get_ssd_enable)
    register_test(test_set_ssd_enable)
    register_test(test_clr_ssd_enable)
    register_test(test_get_ssds)
    register_test(test_get_ssds_diags)
    register_test(test_get_mcdram_capabilities)
    register_test(test_get_mcdram_cfg)
    register_test(test_set_mcdram_cfg)
    register_test(test_clr_mcdram_cfg)
    register_test(test_get_numa_capabilities)
    register_test(test_get_numa_cfg)
    register_test(test_set_numa_cfg)
    register_test(test_clr_numa_cfg)

# strip the version from the url
# NOTE: This assumes the input version is 'v1'
def getVersionlessUrl():
    fullUrl = get_capmc_url()
    pos = fullUrl.find("v1")
    return fullUrl[0:pos]

# test the input endpoint with both 'v1' and base interfaces
def test_url(endPoint):
    # get the base url without a version encoded
    url = getVersionlessUrl()

    # test the v1 interface
    expectedResults = {"http_rc":501, "e":501}
    expectedResults["err_msg"] = "/capmc/v1/" + endPoint + " API Unavailable/Not Implemented"
    retVal = execute_request("POST", url + "v1/" + endPoint, '{ }', expectedResults)

    # test the base interface
    expectedResults["err_msg"] = "/capmc/" + endPoint + " API Unavailable/Not Implemented"
    return retVal + execute_request("POST", url + endPoint, '{ }', expectedResults)

# Below are all the endpoints that are not intended to be implemented 
# as part of the shasta interface.  These tests just make sure it will
# nicely return the http 'not implemented' return code.
def test_get_ssd_enable():
    return test_url("get_ssd_enable")

def test_set_ssd_enable():
    return test_url("set_ssd_enable")

def test_clr_ssd_enable():
    return test_url("clr_ssd_enable")

def test_get_ssds():
    return test_url("get_ssds")

def test_get_ssds_diags():
    return test_url("get_ssd_diags")

def test_get_mcdram_capabilities():
    return test_url("get_mcdram_capabilities")

def test_get_mcdram_cfg():
    return test_url("get_mcdram_cfg")

def test_set_mcdram_cfg():
    return test_url("set_mcdram_cfg")

def test_clr_mcdram_cfg():
    return test_url("clr_mcdram_cfg")

def test_get_numa_capabilities():
    return test_url("get_numa_capabilities")

def test_get_numa_cfg():
    return test_url("get_numa_cfg")

def test_set_numa_cfg():
    return test_url("set_numa_cfg")

def test_clr_numa_cfg():
    return test_url("clr_numa_cfg")
