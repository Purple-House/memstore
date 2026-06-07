package wal

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"os"

	walpb "github.com/odio4u/agni-schema/wal"
	memstore "github.com/odio4u/memstore/seeder/pkg/memstore"
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
		identityBytes := sha256.Sum256([]byte(
			rec.Gateway.VerifiableCredHash + "|" + rec.Gateway.GatewayIp,
		))

		identity := hex.EncodeToString(identityBytes[:])
		gatewayData := &memstore.GatewayData{
			GatewayID:      identity,
			GatewayIP:      rec.Gateway.GatewayIp,
			GatewayPort:    rec.Gateway.GatewayPort,
			GatewayAddress: rec.Gateway.GatewayAddress,
			VerifiableHash: rec.Gateway.VerifiableCredHash,
			Wssport:        rec.Gateway.WssPort,
			Capacity: memstore.Capacity{
				CPU:       rec.Gateway.Capacity.Cpu,
				Memory:    rec.Gateway.Capacity.Memory,
				Storage:   rec.Gateway.Capacity.Storage,
				Bandwidth: rec.Gateway.Capacity.Bandwidth,
			},
		}

		_, err := store.AddGateway(region, gatewayData)
		if err != nil {
			return err
		}
		return nil

	case walpb.Operation_OP_PUT_AGENT:
		region := rec.Agent.Region
		identityBytes := sha256.Sum256([]byte(
			rec.Agent.VerifiableCredHash + "|" + rec.Agent.AgentDomain,
		))

		identity := hex.EncodeToString(identityBytes[:])
		agentData := &memstore.AgentData{
			AgentID:        identity,
			AgentDomain:    rec.Agent.AgentDomain,
			GatewayAddress: rec.Agent.GatewayAddress,
			GatewayID:      rec.Agent.GatewayId,
		}

		_, _, err := store.AddAgent(region, agentData)
		if err != nil {
			return err
		}
		return nil

	case walpb.Operation(3):
		if rec.Agent == nil {
			return fmt.Errorf("delete agent record missing agent payload")
		}
		store.DeleteAgent(rec.Agent.Region, rec.Agent.AgentDomain)
		return nil
	}

	return fmt.Errorf("unknown op: %v", rec.Op)
}
