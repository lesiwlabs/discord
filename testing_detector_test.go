// Code generated by lesiw.io/testdetect. DO NOT EDIT.
package main

func (t testingDetector) Testing() bool { return true }

var _ = (testingDetector{}).testingDetectorEmbed.Testing()
func init() {
	testingDetectorCovHack = true
	defer func() { recover() }()
	testingDetectorInit()
}
