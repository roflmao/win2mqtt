package main

import (
	"testing"
	"unsafe"
)

func TestMibIfRow2Size(t *testing.T) {
	if size := unsafe.Sizeof(mibIfRow2{}); size != 1352 {
		t.Fatalf("sizeof(mibIfRow2) = %d, want 1352", size)
	}
	if off := unsafe.Offsetof(mibIfRow2{}.InOctets); off != 1208 {
		t.Fatalf("offsetof(InOctets) = %d, want 1208", off)
	}
}

func TestSumHardwareCounters(t *testing.T) {
	rows := []mibIfRow2{
		{InterfaceAndOperStatusFlags: ifFlagHardwareInterface, OperStatus: ifOperStatusUp, Type: 6, InOctets: 100, OutOctets: 10},
		{InterfaceAndOperStatusFlags: 0, OperStatus: ifOperStatusUp, Type: 6, InOctets: 999, OutOctets: 999},
		{InterfaceAndOperStatusFlags: ifFlagHardwareInterface, OperStatus: ifOperStatusUp, Type: ifTypeSoftwareLoopback, InOctets: 5, OutOctets: 5},
		{InterfaceAndOperStatusFlags: ifFlagHardwareInterface, OperStatus: 2, Type: 6, InOctets: 7, OutOctets: 7},
	}
	in, out := sumHardwareCounters(rows)
	if in != 100 || out != 10 {
		t.Fatalf("sumHardwareCounters = (%d, %d), want (100, 10)", in, out)
	}
}

func TestMibIfTable2RowsStartAtOffset8(t *testing.T) {
	if off := unsafe.Offsetof(mibIfTable2{}.Table); off != 8 {
		t.Fatalf("offsetof(Table) = %d, want 8", off)
	}
}
