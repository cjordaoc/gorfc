// Example: hello_gorfc connects to a SAP system and calls
// STFC_STRUCTURE to demonstrate ABAP↔Go type conversion.
//
// To run:
//
//	export GORFC_TEST_USER=...      # SAP user (DIALOG or SYSTEM type)
//	export GORFC_TEST_PASSWD=...    # SAP password
//	export GORFC_TEST_ASHOST=...    # application server hostname
//	export GORFC_TEST_SYSNR=00      # system number
//	export GORFC_TEST_CLIENT=100    # SAP client / mandant
//	export GORFC_TEST_LANG=EN       # logon language
//	go run ./example/hello_gorfc.go
//
// Requires the SAP NetWeaver RFC SDK installed (see README.md and
// docs/SECURITY.md). No default credentials are baked into source.

package main

import (
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/cjordaoc/gorfc/gorfc"
)

func abapSystem() gorfc.ConnectionParameters {
	return gorfc.ConnectionParameters{
		"user":   os.Getenv("GORFC_TEST_USER"),
		"passwd": os.Getenv("GORFC_TEST_PASSWD"),
		"ashost": os.Getenv("GORFC_TEST_ASHOST"),
		"sysnr":  os.Getenv("GORFC_TEST_SYSNR"),
		"client": os.Getenv("GORFC_TEST_CLIENT"),
		"lang":   os.Getenv("GORFC_TEST_LANG"),
	}
}

func main() {
	c, err := gorfc.ConnectionFromParams(abapSystem())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Connected:", c.Alive())

	attrs, _ := c.GetConnectionAttributes()
	fmt.Println("Connection attributes", attrs)

	params := map[string]interface{}{
		"IMPORTSTRUCT": map[string]interface{}{
			"RFCFLOAT": 1.23456789,
			"RFCCHAR1": "A",
			"RFCCHAR2": "BC",
			"RFCCHAR4": "ÄBC",
			"RFCINT1":  0xfe,
			"RFCINT2":  0x7ffe,
			"RFCINT4":  999999999,
			"RFCHEX3":  []byte{255, 254, 253},
			"RFCTIME":  time.Now(),
			"RFCDATE":  time.Now(),
			"RFCDATA1": "HELLÖ SÄP",
			"RFCDATA2": "DATA222",
		},
	}
	r, e := c.Call("STFC_STRUCTURE", params)

	if e != nil {
		fmt.Println(e)
		return
	}

	fmt.Println(r["ECHOSTRUCT"])

	importStruct := params["IMPORTSTRUCT"].(map[string]interface{})
	echoStruct := r["ECHOSTRUCT"].(map[string]interface{})
	fmt.Println(echoStruct)

	// empty time
	fmt.Println(importStruct["RFCDATE"], reflect.TypeOf(importStruct["RFCDATE"]))
	fmt.Println(echoStruct["RFCDATE"], reflect.TypeOf(echoStruct["RFCDATE"]))

	// empty date
	fmt.Println(importStruct["RFCTIME"], reflect.TypeOf(importStruct["RFCTIME"]))
	fmt.Println(echoStruct["RFCTIME"], reflect.TypeOf(echoStruct["RFCTIME"]))

	c.Close()
}
