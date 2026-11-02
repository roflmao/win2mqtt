package main

// mibIfRow2 mirrors MIB_IF_ROW2 from netioapi.h (1352 bytes on 64-bit Windows).
// It has no build tag so its layout can be checked by tests on any platform.
type mibIfRow2 struct {
	InterfaceLuid               uint64
	InterfaceIndex              uint32
	InterfaceGuid               [16]byte
	Alias                       [257]uint16
	Description                 [257]uint16
	PhysicalAddressLength       uint32
	PhysicalAddress             [32]byte
	PermanentPhysicalAddress    [32]byte
	Mtu                         uint32
	Type                        uint32
	TunnelType                  uint32
	MediaType                   uint32
	PhysicalMediumType          uint32
	AccessType                  uint32
	DirectionType               uint32
	InterfaceAndOperStatusFlags uint8
	OperStatus                  uint32
	AdminStatus                 uint32
	MediaConnectState           uint32
	NetworkGuid                 [16]byte
	ConnectionType              uint32
	TransmitLinkSpeed           uint64
	ReceiveLinkSpeed            uint64
	InOctets                    uint64
	InUcastPkts                 uint64
	InNUcastPkts                uint64
	InDiscards                  uint64
	InErrors                    uint64
	InUnknownProtos             uint64
	InUcastOctets               uint64
	InMulticastOctets           uint64
	InBroadcastOctets           uint64
	OutOctets                   uint64
	OutUcastPkts                uint64
	OutNUcastPkts               uint64
	OutDiscards                 uint64
	OutErrors                   uint64
	OutUcastOctets              uint64
	OutMulticastOctets          uint64
	OutBroadcastOctets          uint64
	OutQLen                     uint64
}

// mibIfTable2 mirrors MIB_IF_TABLE2; Table is a flexible array of NumEntries rows.
type mibIfTable2 struct {
	NumEntries uint32
	Table      [1]mibIfRow2
}

const (
	ifOperStatusUp          = 1
	ifTypeSoftwareLoopback  = 24
	ifFlagHardwareInterface = 0x01 // InterfaceAndOperStatusFlags.HardwareInterface
)

// sumHardwareCounters adds up bytes received and sent over physical interfaces
// that are up, skipping loopback and software (virtual, filter) interfaces.
func sumHardwareCounters(rows []mibIfRow2) (in, out int64) {
	for i := range rows {
		r := &rows[i]
		if r.InterfaceAndOperStatusFlags&ifFlagHardwareInterface == 0 {
			continue
		}
		if r.OperStatus != ifOperStatusUp || r.Type == ifTypeSoftwareLoopback {
			continue
		}
		in += int64(r.InOctets)
		out += int64(r.OutOctets)
	}
	return in, out
}
