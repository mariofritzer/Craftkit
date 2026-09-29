package main

// Server List Ping: asks a Minecraft server for its version, players and (Forge/NeoForge) mod list,
// exactly like the multiplayer screen does.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ServerMod struct {
	ModID   string `json:"modId"`
	Version string `json:"version"`
}

type ServerInfo struct {
	Address       string      `json:"address"`
	Host          string      `json:"host"`
	Port          int         `json:"port"`
	VersionName   string      `json:"versionName"`
	Protocol      int         `json:"protocol"`
	MCVersion     string      `json:"mcVersion"`    // best guess
	MCCandidates  []string    `json:"mcCandidates"` // all versions matching the protocol
	MultiVersion  bool        `json:"multiVersion"` // server accepts a range (ViaVersion, proxies)
	Loader        string      `json:"loader"`       // forge, neoforge, fabric, "" (vanilla/paper/unknown)
	Software      string      `json:"software"`     // Paper, Purpur, Velocity … from version name
	MOTD          string      `json:"motd"`
	PlayersOnline int         `json:"playersOnline"`
	PlayersMax    int         `json:"playersMax"`
	Favicon       string      `json:"favicon,omitempty"`
	Mods          []ServerMod `json:"mods"`
	ModsTruncated bool        `json:"modsTruncated"`
	PingMs        int64       `json:"pingMs"`
}

// protocol numbers of release versions (newest first within a protocol)
var protocolVersions = map[int][]string{
	774: {"1.21.11"}, 773: {"1.21.10", "1.21.9"}, 772: {"1.21.8", "1.21.7"}, 771: {"1.21.6"}, 770: {"1.21.5"},
	769: {"1.21.4"}, 768: {"1.21.3", "1.21.2"}, 767: {"1.21.1", "1.21"}, 766: {"1.20.6", "1.20.5"},
	765: {"1.20.4", "1.20.3"}, 764: {"1.20.2"}, 763: {"1.20.1", "1.20"}, 762: {"1.19.4"}, 761: {"1.19.3"},
	760: {"1.19.2", "1.19.1"}, 759: {"1.19"}, 758: {"1.18.2"}, 757: {"1.18.1", "1.18"}, 756: {"1.17.1"},
	755: {"1.17"}, 754: {"1.16.5", "1.16.4"}, 753: {"1.16.3"}, 751: {"1.16.2"}, 736: {"1.16.1"}, 735: {"1.16"},
	578: {"1.15.2"}, 575: {"1.15.1"}, 573: {"1.15"}, 498: {"1.14.4"}, 404: {"1.13.2"}, 340: {"1.12.2"},
	338: {"1.12.1"}, 335: {"1.12"}, 316: {"1.11.2"}, 315: {"1.11"}, 210: {"1.10.2"}, 110: {"1.9.4"}, 47: {"1.8.9"},
}

func parseAddress(addr string) (host string, port int, explicitPort bool, err error) {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "minecraft://"), "mc://")
	if addr == "" {
		return "", 0, false, errors.New("bitte eine Server-Adresse eingeben")
	}
	host, portStr, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return addr, 25565, false, nil
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 || p > 65535 {
		return "", 0, false, errors.New("ungültiger Port")
	}
	return host, p, true, nil
}

func pingServer(addr string) (*ServerInfo, error) {
	host, port, explicit, err := parseAddress(addr)
	if err != nil {
		return nil, err
	}
	connectHost, connectPort := host, port
	if !explicit && net.ParseIP(host) == nil {
		// many servers use an SRV record (_minecraft._tcp.<host>)
		if _, srvs, err := net.LookupSRV("minecraft", "tcp", host); err == nil && len(srvs) > 0 {
			connectHost = strings.TrimSuffix(srvs[0].Target, ".")
			connectPort = int(srvs[0].Port)
		}
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(connectHost, strconv.Itoa(connectPort)), 6*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Server %s nicht erreichbar (%v)", addr, simplifyNetErr(err))
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(8 * time.Second))

	// handshake: id 0, protocol -1, host, port, next state 1 (status)
	var hs bytes.Buffer
	writeVarInt(&hs, 0)
	writeVarInt(&hs, -1)
	writeString(&hs, host)
	binary.Write(&hs, binary.BigEndian, uint16(port))
	writeVarInt(&hs, 1)
	var status bytes.Buffer
	writeVarInt(&status, 0)
	var out bytes.Buffer
	writePacket(&out, hs.Bytes())
	writePacket(&out, status.Bytes())
	if _, err := conn.Write(out.Bytes()); err != nil {
		return nil, err
	}
	r := bufio.NewReader(conn)
	if _, err := readVarInt(r); err != nil { // packet length
		return nil, fmt.Errorf("keine Antwort vom Server (ist das ein Minecraft-Java-Server?)")
	}
	if id, err := readVarInt(r); err != nil || id != 0 {
		return nil, fmt.Errorf("unerwartete Antwort vom Server")
	}
	n, err := readVarInt(r)
	if err != nil || n <= 0 || n > 8<<20 {
		return nil, fmt.Errorf("ungültige Antwort vom Server")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	info, err := parseStatusJSON(buf)
	if err != nil {
		return nil, err
	}
	info.Address, info.Host, info.Port = addr, host, port
	info.PingMs = time.Since(start).Milliseconds()
	return info, nil
}

func simplifyNetErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return "Adresse unbekannt"
	case strings.Contains(s, "refused"):
		return "Verbindung abgelehnt"
	case strings.Contains(s, "timeout"):
		return "Zeitüberschreitung"
	}
	return s
}

var mcVersionRe = regexp.MustCompile(`\b(1\.\d{1,2}(?:\.\d{1,2})?|2\d\.\d{1,2}(?:\.\d{1,2})?)\b`)

func parseStatusJSON(b []byte) (*ServerInfo, error) {
	var st struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Online int `json:"online"`
			Max    int `json:"max"`
		} `json:"players"`
		Description json.RawMessage `json:"description"`
		Favicon     string          `json:"favicon"`
		ForgeData   *struct {
			Mods []struct {
				ModID  string `json:"modId"`
				Marker string `json:"modmarker"`
			} `json:"mods"`
			D         string `json:"d"`
			Truncated bool   `json:"truncated"`
		} `json:"forgeData"`
		ModInfo *struct {
			Type    string `json:"type"`
			ModList []struct {
				ModID   string `json:"modid"`
				Version string `json:"version"`
			} `json:"modList"`
		} `json:"modinfo"`
		IsModded            bool `json:"isModded"` // NeoForge
		PreventsChatReports bool `json:"preventsChatReports"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("Server-Antwort nicht lesbar: %w", err)
	}
	info := &ServerInfo{VersionName: stripFormatting(st.Version.Name), Protocol: st.Version.Protocol,
		PlayersOnline: st.Players.Online, PlayersMax: st.Players.Max, Favicon: st.Favicon}
	info.MOTD = strings.TrimSpace(stripFormatting(chatText(st.Description)))

	// software from the version name ("Paper 1.21.1", "Velocity 3.3.0", "1.20.1")
	name := info.VersionName
	if m := regexp.MustCompile(`^([A-Za-z][A-Za-z\- ]+?)\s+\d`).FindStringSubmatch(name); m != nil {
		info.Software = strings.TrimSpace(m[1])
	}
	found := mcVersionRe.FindAllString(name, -1)
	info.MultiVersion = len(found) > 1 || strings.Contains(name, ".x") || strings.Contains(name, "-1.")
	info.MCCandidates = protocolVersions[st.Version.Protocol]
	switch {
	case len(found) == 1 && !info.MultiVersion:
		info.MCVersion = found[0]
	case len(info.MCCandidates) > 0:
		info.MCVersion = info.MCCandidates[0]
	case len(found) > 0:
		info.MCVersion = found[len(found)-1]
	}
	if info.MCVersion != "" && !contains(info.MCCandidates, info.MCVersion) {
		info.MCCandidates = append([]string{info.MCVersion}, info.MCCandidates...)
	}

	// mods
	add := func(id, ver string) {
		l := strings.ToLower(id)
		if l == "" || l == "minecraft" || l == "forge" || l == "neoforge" || l == "fml" || l == "mcp" {
			return
		}
		info.Mods = append(info.Mods, ServerMod{ModID: id, Version: ver})
	}
	if st.ForgeData != nil {
		info.Loader = "forge"
		if st.ForgeData.D != "" {
			mods, truncated, err := decodeForgeData(st.ForgeData.D)
			if err == nil {
				for _, m := range mods {
					add(m.ModID, m.Version)
				}
				info.ModsTruncated = truncated
			}
		} else {
			for _, m := range st.ForgeData.Mods {
				if m.Marker != "SERVER" && !strings.Contains(m.Marker, "IGNORESERVERONLY") {
					add(m.ModID, m.Marker)
				}
			}
			info.ModsTruncated = st.ForgeData.Truncated
		}
	}
	if st.ModInfo != nil && strings.EqualFold(st.ModInfo.Type, "FML") {
		info.Loader = "forge"
		for _, m := range st.ModInfo.ModList {
			add(m.ModID, m.Version)
		}
	}
	for _, m := range info.Mods {
		if strings.EqualFold(m.ModID, "neoforge") {
			info.Loader = "neoforge"
		}
	}
	low := strings.ToLower(name)
	switch {
	case strings.Contains(low, "neoforge"), st.IsModded && info.Loader == "":
		info.Loader = "neoforge"
	case strings.Contains(low, "fabric"):
		info.Loader = "fabric"
	case strings.Contains(low, "quilt"):
		info.Loader = "quilt"
	}
	sort.Slice(info.Mods, func(i, k int) bool { return info.Mods[i].ModID < info.Mods[k].ModID })
	return info, nil
}

// decodeForgeData decodes the compact mod list Forge 1.18+ puts into forgeData.d.
func decodeForgeData(s string) (mods []ServerMod, truncated bool, err error) {
	rs := []rune(s)
	if len(rs) < 2 {
		return nil, false, errors.New("zu kurz")
	}
	size := int(rs[0]) | int(rs[1])<<15
	var out []byte
	var buffer uint64
	bits := 0
	for i := 2; i < len(rs); i++ {
		for bits >= 8 {
			out = append(out, byte(buffer))
			buffer >>= 8
			bits -= 8
		}
		buffer |= uint64(rs[i]&0x7FFF) << bits
		bits += 15
	}
	for len(out) < size {
		out = append(out, byte(buffer))
		buffer >>= 8
		bits -= 8
	}
	out = out[:size]
	r := bytes.NewReader(out)
	tb, err := r.ReadByte()
	if err != nil {
		return nil, false, err
	}
	truncated = tb != 0
	var count uint16
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, truncated, err
	}
	for i := 0; i < int(count); i++ {
		flag, err := readVarInt(r)
		if err != nil {
			return mods, truncated, err
		}
		channels := flag >> 1
		ignoreServerOnly := flag&1 != 0
		id, err := readString(r)
		if err != nil {
			return mods, truncated, err
		}
		ver := ""
		if !ignoreServerOnly {
			if ver, err = readString(r); err != nil {
				return mods, truncated, err
			}
		}
		for c := 0; c < channels; c++ {
			if _, err := readString(r); err != nil {
				return mods, truncated, err
			}
			if _, err := readString(r); err != nil {
				return mods, truncated, err
			}
			if _, err := r.ReadByte(); err != nil {
				return mods, truncated, err
			}
		}
		if !ignoreServerOnly { // server-only mods are not needed on the client
			mods = append(mods, ServerMod{ModID: id, Version: ver})
		}
	}
	return mods, truncated, nil
}

func chatText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var c struct {
		Text  string            `json:"text"`
		Extra []json.RawMessage `json:"extra"`
	}
	if json.Unmarshal(raw, &c) != nil {
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) == nil {
			var sb strings.Builder
			for _, a := range arr {
				sb.WriteString(chatText(a))
			}
			return sb.String()
		}
		return ""
	}
	var sb strings.Builder
	sb.WriteString(c.Text)
	for _, e := range c.Extra {
		sb.WriteString(chatText(e))
	}
	return sb.String()
}

var formatRe = regexp.MustCompile(`§.`)

func stripFormatting(s string) string { return formatRe.ReplaceAllString(s, "") }

// ---------- protocol helpers ----------

func writeVarInt(w *bytes.Buffer, v int) {
	u := uint32(int32(v))
	for {
		if u&^0x7F == 0 {
			w.WriteByte(byte(u))
			return
		}
		w.WriteByte(byte(u&0x7F | 0x80))
		u >>= 7
	}
}

func writeString(w *bytes.Buffer, s string) {
	writeVarInt(w, len(s))
	w.WriteString(s)
}

func writePacket(w *bytes.Buffer, payload []byte) {
	writeVarInt(w, len(payload))
	w.Write(payload)
}

func readVarInt(r io.ByteReader) (int, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int(int32(v)), nil
		}
	}
	return 0, errors.New("VarInt zu lang")
}

func readString(r *bytes.Reader) (string, error) {
	n, err := readVarInt(r)
	if err != nil {
		return "", err
	}
	if n < 0 || n > r.Len() {
		return "", errors.New("ungültige Länge")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}
