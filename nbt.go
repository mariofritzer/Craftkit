package main

// Minimal NBT reader/writer, enough to add a server to servers.dat without losing existing entries.

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type nbtTag struct {
	Type  byte
	Name  string
	Value any // compound: []*nbtTag, list: nbtList, primitives, []byte, []int32, []int64, string
}

type nbtList struct {
	ElemType byte
	Items    []any
}

func nbtReadPayload(r *bytes.Reader, t byte, depth int) (any, error) {
	if depth > 64 {
		return nil, errNew("NBT zu tief verschachtelt")
	}
	var err error
	switch t {
	case 1:
		var v int8
		err = binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 2:
		var v int16
		err = binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 3:
		var v int32
		err = binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 4:
		var v int64
		err = binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 5:
		var v float32
		err = binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 6:
		var v float64
		err = binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 7:
		var n int32
		if err = binary.Read(r, binary.BigEndian, &n); err != nil || n < 0 || int(n) > r.Len() {
			return nil, errNew("ungültiges Byte-Array")
		}
		b := make([]byte, n)
		_, err = io.ReadFull(r, b)
		return b, err
	case 8:
		var n uint16
		if err = binary.Read(r, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		b := make([]byte, n)
		_, err = io.ReadFull(r, b)
		return string(b), err
	case 9:
		var et byte
		var n int32
		if et, err = r.ReadByte(); err != nil {
			return nil, err
		}
		if err = binary.Read(r, binary.BigEndian, &n); err != nil || n < 0 || int(n) > r.Len()+1 {
			return nil, errNew("ungültige Liste")
		}
		l := nbtList{ElemType: et}
		for i := 0; i < int(n); i++ {
			v, err := nbtReadPayload(r, et, depth+1)
			if err != nil {
				return nil, err
			}
			l.Items = append(l.Items, v)
		}
		return l, nil
	case 10:
		var tags []*nbtTag
		for {
			tt, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			if tt == 0 {
				return tags, nil
			}
			var n uint16
			if err := binary.Read(r, binary.BigEndian, &n); err != nil {
				return nil, err
			}
			nb := make([]byte, n)
			if _, err := io.ReadFull(r, nb); err != nil {
				return nil, err
			}
			v, err := nbtReadPayload(r, tt, depth+1)
			if err != nil {
				return nil, err
			}
			tags = append(tags, &nbtTag{tt, string(nb), v})
		}
	case 11:
		var n int32
		if err = binary.Read(r, binary.BigEndian, &n); err != nil || n < 0 || int(n)*4 > r.Len() {
			return nil, errNew("ungültiges Int-Array")
		}
		v := make([]int32, n)
		err = binary.Read(r, binary.BigEndian, v)
		return v, err
	case 12:
		var n int32
		if err = binary.Read(r, binary.BigEndian, &n); err != nil || n < 0 || int(n)*8 > r.Len() {
			return nil, errNew("ungültiges Long-Array")
		}
		v := make([]int64, n)
		err = binary.Read(r, binary.BigEndian, v)
		return v, err
	}
	return nil, errNew("unbekannter NBT-Typ")
}

func nbtWritePayload(w *bytes.Buffer, t byte, v any) {
	switch t {
	case 1, 2, 3, 4, 5, 6:
		binary.Write(w, binary.BigEndian, v)
	case 7:
		b := v.([]byte)
		binary.Write(w, binary.BigEndian, int32(len(b)))
		w.Write(b)
	case 8:
		s := v.(string)
		binary.Write(w, binary.BigEndian, uint16(len(s)))
		w.WriteString(s)
	case 9:
		l := v.(nbtList)
		et := l.ElemType
		if len(l.Items) == 0 && et == 0 {
			et = 0
		}
		w.WriteByte(et)
		binary.Write(w, binary.BigEndian, int32(len(l.Items)))
		for _, it := range l.Items {
			nbtWritePayload(w, et, it)
		}
	case 10:
		for _, tg := range v.([]*nbtTag) {
			w.WriteByte(tg.Type)
			binary.Write(w, binary.BigEndian, uint16(len(tg.Name)))
			w.WriteString(tg.Name)
			nbtWritePayload(w, tg.Type, tg.Value)
		}
		w.WriteByte(0)
	case 11:
		a := v.([]int32)
		binary.Write(w, binary.BigEndian, int32(len(a)))
		binary.Write(w, binary.BigEndian, a)
	case 12:
		a := v.([]int64)
		binary.Write(w, binary.BigEndian, int32(len(a)))
		binary.Write(w, binary.BigEndian, a)
	}
}

// addServerToList adds (or keeps) a server entry in <gameDir>/servers.dat.
// Returns false if the address was already listed.
func addServerToList(gameDir, name, address string) (bool, error) {
	path := filepath.Join(gameDir, "servers.dat")
	root := []*nbtTag{}
	if b, err := os.ReadFile(path); err == nil && len(b) > 3 {
		r := bytes.NewReader(b)
		t, _ := r.ReadByte()
		var n uint16
		binary.Read(r, binary.BigEndian, &n)
		r.Seek(int64(n), io.SeekCurrent)
		if t != 10 {
			return false, errNew("servers.dat hat ein unbekanntes Format")
		}
		v, err := nbtReadPayload(r, 10, 0)
		if err != nil {
			return false, errNew("servers.dat ist beschädigt – bitte im Spiel öffnen oder löschen")
		}
		root = v.([]*nbtTag)
	}
	var servers *nbtTag
	for _, t := range root {
		if t.Name == "servers" && t.Type == 9 {
			servers = t
		}
	}
	if servers == nil {
		servers = &nbtTag{Type: 9, Name: "servers", Value: nbtList{ElemType: 10}}
		root = append(root, servers)
	}
	list := servers.Value.(nbtList)
	if list.ElemType == 0 {
		list.ElemType = 10
	}
	for _, it := range list.Items {
		if tags, ok := it.([]*nbtTag); ok {
			for _, t := range tags {
				if t.Name == "ip" {
					if s, _ := t.Value.(string); strings.EqualFold(s, address) {
						return false, nil
					}
				}
			}
		}
	}
	list.Items = append(list.Items, []*nbtTag{
		{Type: 8, Name: "name", Value: name},
		{Type: 8, Name: "ip", Value: address},
	})
	servers.Value = list
	var w bytes.Buffer
	w.WriteByte(10)
	binary.Write(&w, binary.BigEndian, uint16(0))
	nbtWritePayload(&w, 10, root)
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return false, err
	}
	if fileExists(path) {
		os.WriteFile(path+".craftkit-backup", mustRead(path), 0o644)
	}
	return true, os.WriteFile(path, w.Bytes(), 0o644)
}

func mustRead(p string) []byte {
	b, _ := os.ReadFile(p)
	return b
}
