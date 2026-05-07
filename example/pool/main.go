// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

//go:build (linux || darwin || windows) && cgo && !nwrfc_nosdk

// Example: ping the SAP system N times via a Pool.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"sync"
	"time"

	"github.com/cjordaoc/gorfc/nwrfc"
)

func main() {
	calls := flag.Int("calls", 100, "calls to issue")
	gor := flag.Int("g", 16, "concurrent goroutines")
	flag.Parse()

	if err := nwrfc.EnsureSDK(); err != nil {
		log.Fatalf("nwrfc: %v", err)
	}

	pool, err := nwrfc.NewPool(nwrfc.PoolConfig{
		Params: nwrfc.Params{
			AsHost: os.Getenv("GORFC_TEST_ASHOST"),
			SysNr:  os.Getenv("GORFC_TEST_SYSNR"),
			Client: os.Getenv("GORFC_TEST_CLIENT"),
			User:   os.Getenv("GORFC_TEST_USER"),
			Passwd: os.Getenv("GORFC_TEST_PASSWD"),
			Lang:   os.Getenv("GORFC_TEST_LANG"),
		},
		MinSize:        2,
		MaxSize:        *gor,
		IdleTimeout:    5 * time.Minute,
		AcquireTimeout: 5 * time.Second,
		AfterAcquire: func(ctx context.Context, c *nwrfc.Conn) error {
			return c.Reset()
		},
	})
	if err != nil {
		log.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(*gor)
	for i := 0; i < *gor; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < *calls/(*gor); j++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := pool.Do(ctx, func(c *nwrfc.Conn) error {
					return c.Ping(ctx)
				})
				cancel()
				if err != nil {
					log.Printf("Ping: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	stats := pool.Stats()
	log.Printf("done %d calls in %v (open=%d idle=%d)", *calls, elapsed, stats.Open, stats.Idle)
}
