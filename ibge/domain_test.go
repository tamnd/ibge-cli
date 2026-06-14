package ibge

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, resolve), which need no network. The client's
// HTTP behaviour is covered in ibge_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "ibge" {
		t.Errorf("Scheme = %q, want ibge", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "ibge" {
		t.Errorf("Identity.Binary = %q, want ibge", info.Identity.Binary)
	}
}

func TestClassifyNumeric(t *testing.T) {
	typ, id, err := Domain{}.Classify("1100015")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != "municipality" || id != "1100015" {
		t.Errorf("Classify(\"1100015\") = (%q, %q), want (municipality, 1100015)", typ, id)
	}
}

func TestClassifyState(t *testing.T) {
	cases := []struct{ in, wantID string }{
		{"SP", "SP"},
		{"RO", "RO"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != "state" || id != tc.wantID {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (state, %s, nil)", tc.in, typ, id, err, tc.wantID)
		}
	}
}

func TestClassifyName(t *testing.T) {
	cases := []string{"jose", "Maria", "João Silva"}
	for _, c := range cases {
		typ, id, err := Domain{}.Classify(c)
		if err != nil || typ != "name" || id != c {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (name, %s, nil)", c, typ, id, err, c)
		}
	}
}

func TestLocateState(t *testing.T) {
	got, err := Domain{}.Locate("state", "SP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://www.ibge.gov.br/cidades-e-estados/sp.html"
	if got != want {
		t.Errorf("Locate(state, SP) = %q, want %q", got, want)
	}
}

func TestLocateMunicipality(t *testing.T) {
	got, err := Domain{}.Locate("municipality", "1100015")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://www.ibge.gov.br/cidades-e-estados" {
		t.Errorf("Locate(municipality, 1100015) = %q", got)
	}
}

func TestLocateFallback(t *testing.T) {
	got, err := Domain{}.Locate("name", "jose")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://www.ibge.gov.br" {
		t.Errorf("Locate(name, jose) = %q", got)
	}
}

func TestHostWiringDomains(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range h.Domains() {
		if d == "ibge" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ibge domain not registered; domains = %v", h.Domains())
	}
}

func TestHostWiringResolveOn(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}
	// Classify("SP") → (state, SP), so ResolveOn should work
	got, err := h.ResolveOn("ibge", "SP")
	if err != nil {
		t.Fatalf("ResolveOn: %v", err)
	}
	if got.String() != "ibge://state/SP" {
		t.Errorf("ResolveOn = %q, want ibge://state/SP", got.String())
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return an error")
	}
}
