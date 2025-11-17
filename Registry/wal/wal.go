package wal

import (
	"bufio"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"sync"

	walpb "github.com/Purple-House/memstore/registry/wal/proto"
	"google.golang.org/protobuf/proto"
)

const (
	Magic   uint16 = 0xCAFE
	version byte   = 1

	walFile = "wal.log"
	// walRotated  = "wal.log.1" not used yet
	// maxWalBytes = 32 * 1024 * 1024 // 32MB
)

var ErrCorrupt = errors.New("wal corruption detected")

type WALer struct {
	mu     sync.Mutex
	f      *os.File
	path   string
	writer *bufio.Writer
}

func OpenWAl() (*WALer, error) {
	f, err := os.OpenFile(walFile, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	return &WALer{
		f:      f,
		writer: bufio.NewWriter(f),
	}, nil
}

func (w *WALer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.writer.Flush(); err != nil {
		return err
	}
	return w.f.Close()
}

func (w *WALer) Append(rec *walpb.WalRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := proto.Marshal(rec)
	if err != nil {
		return err
	}

	crc := crc32.ChecksumIEEE(data)

	header := make([]byte, 8)
	binary.BigEndian.PutUint16(header[0:2], Magic)
	header[2] = version
	header[3] = byte(rec.Op)
	binary.BigEndian.PutUint32(header[4:8], uint32(len(data)))

	if _, err := w.writer.Write(header); err != nil {
		return err
	}

	if _, err := w.writer.Write(data); err != nil {
		return err
	}

	crcBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBuf, crc)
	if _, err := w.writer.Write(crcBuf); err != nil {
		return err
	}

	return w.writer.Flush()
}
