package main

import "testing"

func TestWindowsDaemonMemorySampling(t *testing.T) {
	if total := physicalRAMBytes(); total == 0 {
		t.Fatal("physical RAM unavailable")
	}
	sample, err := daemonSampler().Sample(0)
	if err != nil || sample.RSSBytes == 0 {
		t.Fatalf("daemon sample %+v: %v", sample, err)
	}
}
