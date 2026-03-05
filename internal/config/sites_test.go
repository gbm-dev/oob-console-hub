package config

import (
	"strings"
	"testing"
)

func TestParseSites(t *testing.T) {
	input := `# Header comment
# Format: name|phone|description|baud

2broadway|14105551234|2 Broadway Terminal Server|19200
router1|13125559876|Chicago Core Router|9600

# Another comment

nyc-switch|12125551111|NYC Core Switch Stack|38400
`
	sites, err := ParseSites(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 3 {
		t.Fatalf("expected 3 sites, got %d", len(sites))
	}

	tests := []struct {
		name, phone, desc string
		baud              int
	}{
		{"2broadway", "14105551234", "2 Broadway Terminal Server", 19200},
		{"router1", "13125559876", "Chicago Core Router", 9600},
		{"nyc-switch", "12125551111", "NYC Core Switch Stack", 38400},
	}
	for i, tt := range tests {
		s := sites[i]
		if s.Name != tt.name {
			t.Errorf("site %d: name = %q, want %q", i, s.Name, tt.name)
		}
		if s.Phone != tt.phone {
			t.Errorf("site %d: phone = %q, want %q", i, s.Phone, tt.phone)
		}
		if s.Description != tt.desc {
			t.Errorf("site %d: desc = %q, want %q", i, s.Description, tt.desc)
		}
		if s.BaudRate != tt.baud {
			t.Errorf("site %d: baud = %d, want %d", i, s.BaudRate, tt.baud)
		}
		if len(s.ModemInit) != 0 {
			t.Errorf("site %d: expected no modem init commands, got %v", i, s.ModemInit)
		}
	}
}

func TestParseSitesWithModemInit(t *testing.T) {
	input := `HQ-Core|15551234567|Main datacenter|9600|AT+MS=132,0,4800,9600
plain-site|15559876543|No modem init|19200
multi-cmd|15551111111|Multiple commands|9600|AT+MS=132,0,4800,9600;ATS7=60
`
	sites, err := ParseSites(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 3 {
		t.Fatalf("expected 3 sites, got %d", len(sites))
	}

	// Site with one modem init command
	if len(sites[0].ModemInit) != 1 {
		t.Fatalf("site 0: expected 1 modem init command, got %d", len(sites[0].ModemInit))
	}
	if sites[0].ModemInit[0] != "AT+MS=132,0,4800,9600" {
		t.Errorf("site 0: modem init = %q, want %q", sites[0].ModemInit[0], "AT+MS=132,0,4800,9600")
	}

	// Site with no modem init (4-field line)
	if len(sites[1].ModemInit) != 0 {
		t.Errorf("site 1: expected no modem init, got %v", sites[1].ModemInit)
	}

	// Site with two modem init commands
	if len(sites[2].ModemInit) != 2 {
		t.Fatalf("site 2: expected 2 modem init commands, got %d", len(sites[2].ModemInit))
	}
	if sites[2].ModemInit[0] != "AT+MS=132,0,4800,9600" {
		t.Errorf("site 2: modem init[0] = %q", sites[2].ModemInit[0])
	}
	if sites[2].ModemInit[1] != "ATS7=60" {
		t.Errorf("site 2: modem init[1] = %q", sites[2].ModemInit[1])
	}
}

func TestParseSitesFile(t *testing.T) {
	sites, err := ParseSitesFile("../../tests/fixtures/oob-sites.conf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 3 {
		t.Fatalf("expected 3 sites, got %d", len(sites))
	}
	if sites[0].Name != "2broadway" {
		t.Errorf("first site name = %q, want %q", sites[0].Name, "2broadway")
	}
}

func TestParseSitesEmpty(t *testing.T) {
	sites, err := ParseSites(strings.NewReader("# only comments\n\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 sites, got %d", len(sites))
	}
}

func TestParseSitesInvalidFields(t *testing.T) {
	_, err := ParseSites(strings.NewReader("bad|line\n"))
	if err == nil {
		t.Fatal("expected error for invalid field count")
	}
}

func TestParseSitesInvalidBaud(t *testing.T) {
	_, err := ParseSites(strings.NewReader("name|phone|desc|notanumber\n"))
	if err == nil {
		t.Fatal("expected error for invalid baud rate")
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Test defaults
	cfg := LoadFromEnv()
	if cfg.SSHPort != 2222 {
		t.Errorf("default SSHPort = %d, want 2222", cfg.SSHPort)
	}
	if cfg.DevicePath != "/dev/ttySL0" {
		t.Errorf("default DevicePath = %q, want /dev/ttySL0", cfg.DevicePath)
	}

	// Test override
	t.Setenv("SSH_PORT", "3333")
	t.Setenv("DEVICE_PATH", "/dev/ttyUSB0")
	cfg = LoadFromEnv()
	if cfg.SSHPort != 3333 {
		t.Errorf("SSHPort = %d, want 3333", cfg.SSHPort)
	}
	if cfg.DevicePath != "/dev/ttyUSB0" {
		t.Errorf("DevicePath = %q, want /dev/ttyUSB0", cfg.DevicePath)
	}
}
