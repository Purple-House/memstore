package wal

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"

	memstore "github.com/Purple-House/memstore/registry/pkg/memstore"
	walpb "github.com/Purple-House/memstore/registry/wal/proto"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

func (w *WALer) Replay(apply func(*walpb.WalRecord) error) error {

	f, err := os.Open(w.path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReader(f)

	for {
		header := make([]byte, 8)
		_, err := io.ReadFull(r, header)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		magic := binary.BigEndian.Uint16(header[0:])
		if magic != Magic {
			return fmt.Errorf("%w: invalid magic", ErrCorrupt)
		}

		op := walpb.Operation(header[3])
		size := binary.BigEndian.Uint32(header[4:])

		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			return err
		}

		crcBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, crcBuf); err != nil {
			return err
		}

		expectedCRC := binary.BigEndian.Uint32(crcBuf)
		actualCRC := crc32.ChecksumIEEE(payload)
		if expectedCRC != actualCRC {
			return fmt.Errorf("%w: crc mismatch", ErrCorrupt)
		}

		rec := &walpb.WalRecord{
			Op: op,
		}
		if err := proto.Unmarshal(payload, rec); err != nil {
			return err
		}

		if err := apply(rec); err != nil {
			return err
		}
	}
}

func ApplyRecord(store *memstore.MemStore, rec *walpb.WalRecord) error {
	switch rec.Op {

	case walpb.Operation_OP_PUT_GATEWAY:
		region := rec.Gateway.Region
		data := store.RegionExist(region)

		data.Mu.Lock()
		data.Gateways[rec.Gateway.GatewayDomain] = &memstore.GatewayData{
			GatewayID:     uuid.NewString(),
			GatewayIP:     rec.Gateway.GatewayIp,
			GatewayDomain: rec.Gateway.GatewayDomain,
			Capacity: memstore.Capacity{
				CPU:       rec.Gateway.Capacity.Cpu,
				Memory:    rec.Gateway.Capacity.Memory,
				Storage:   rec.Gateway.Capacity.Storage,
				Bandwidth: rec.Gateway.Capacity.Bandwidth,
			},
		}
		data.Mu.Unlock()
		return nil

	case walpb.Operation_OP_PUT_AGENT:
		region := rec.Agent.Region
		data := store.RegionExist(region)

		data.Mu.Lock()
		data.Agents[rec.Agent.AgentDomain] = &memstore.AgentData{
			AgentID:       uuid.NewString(),
			AgentDomain:   rec.Agent.AgentDomain,
			GatewayID:     rec.Agent.GatewayId,
			GatewayDomain: rec.Agent.Domain,
		}
		data.Mu.Unlock()
		return nil
	}

	return fmt.Errorf("unknown op: %v", rec.Op)
}
