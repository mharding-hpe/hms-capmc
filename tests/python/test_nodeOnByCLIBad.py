#!/usr/bin/python3
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
"""
Test case for nodeOnByCLIBad
"""
from sys import exit

from capmcLib import nodePower

################################################################################
#
#   nodeOnByCLIBad
#
################################################################################
def nodeOnByCLIBad():
    TEST = "nodeOnByCLIBad"
    nid = "99999"
    ON = "on"

    print("["+TEST+"] Test powering on of bad node "+nid)

    # Power on node
    print("["+TEST+"] Powering on nid "+nid)
    ret = nodePower(ON, nid)
    if ret["errcode"] != 0:
        print("["+TEST+"] PASS: expected failure when trying to power on nid "+nid+": "+ret["errstr"])
        return 0

    print("["+TEST+"] FAIL: Did not receive a failure when trying to power on node "+nid)
    return 1

def test_nodeOnByCLIBad():
    assert nodeOnByCLIBad() == 0

if __name__ == "__main__":
    ret = nodeOnByCLIBad()
    exit(ret)