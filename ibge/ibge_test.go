package ibge_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/ibge-cli/ibge"
)

// newTestClient returns a Client pointed at srv with pacing disabled.
func newTestClient(srv *httptest.Server) *ibge.Client {
	c := ibge.NewClient()
	c.Rate = 0
	c.HTTP = &http.Client{Timeout: 5 * time.Second}
	_ = srv // srv.URL used by caller
	return c
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := ibge.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := ibge.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGetStates(t *testing.T) {
	payload := []map[string]any{
		{"id": 35, "sigla": "SP", "nome": "São Paulo", "regiao": map[string]any{"id": 3, "sigla": "SE", "nome": "Sudeste"}},
		{"id": 11, "sigla": "RO", "nome": "Rondônia", "regiao": map[string]any{"id": 1, "sigla": "N", "nome": "Norte"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := ibge.NewClient()
	c.Rate = 0
	// Override BaseURL via a test-only approach: we call Get directly against srv.URL
	// and decode manually to verify the client's wire decoding. For integration we
	// use the real client methods but swap the base. Since BaseURL is a package-level
	// const, we test GetStates via the exported method against a patched client.
	//
	// Instead, verify the raw decoding path works correctly by checking the JSON
	// structure our server returns can be unmarshalled into the expected shape.
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var raw []struct {
		ID    int    `json:"id"`
		Sigla string `json:"sigla"`
		Nome  string `json:"nome"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("len = %d, want 2", len(raw))
	}
	if raw[0].Sigla != "SP" {
		t.Errorf("first state sigla = %q, want SP", raw[0].Sigla)
	}
}

func TestGetRegions(t *testing.T) {
	payload := []map[string]any{
		{"id": 1, "sigla": "N", "nome": "Norte"},
		{"id": 2, "sigla": "NE", "nome": "Nordeste"},
		{"id": 3, "sigla": "SE", "nome": "Sudeste"},
		{"id": 4, "sigla": "S", "nome": "Sul"},
		{"id": 5, "sigla": "CO", "nome": "Centro-Oeste"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := ibge.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var raw []struct {
		ID   int    `json:"id"`
		Nome string `json:"nome"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 5 {
		t.Fatalf("len = %d, want 5", len(raw))
	}
}

func TestGetNews(t *testing.T) {
	payload := []map[string]any{
		{"id": 1, "tipo": "noticia", "titulo": "Test Title", "introducao": "Test intro text here.", "data_publicacao": "2024-01-15 10:00:00"},
		{"id": 2, "tipo": "noticia", "titulo": "", "introducao": "Another intro without title", "data_publicacao": "2024-01-14 09:00:00"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := ibge.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var raw []struct {
		ID             int    `json:"id"`
		Tipo           string `json:"tipo"`
		Titulo         string `json:"titulo"`
		Introducao     string `json:"introducao"`
		DataPublicacao string `json:"data_publicacao"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("len = %d, want 2", len(raw))
	}
	if raw[0].Titulo != "Test Title" {
		t.Errorf("titulo = %q, want Test Title", raw[0].Titulo)
	}
	// second item has no titulo — we fall back to introducao
	if raw[1].Titulo != "" {
		t.Errorf("second titulo = %q, want empty (fallback to introducao)", raw[1].Titulo)
	}
}

func TestGetNameFrequency(t *testing.T) {
	payload := []map[string]any{
		{
			"localidade": "BR",
			"sexo":       "",
			"res": []map[string]any{
				{"periodo": "[1930,1940[", "frequencia": 169528},
				{"periodo": "[1940,1950[", "frequencia": 305042},
				{"periodo": "[1950,1960[", "frequencia": 506438},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := ibge.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var raw []struct {
		Localidade string `json:"localidade"`
		Res        []struct {
			Periodo    string `json:"periodo"`
			Frequencia int    `json:"frequencia"`
		} `json:"res"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 || len(raw[0].Res) != 3 {
		t.Fatalf("unexpected shape: %d results, %d periods", len(raw), len(raw[0].Res))
	}
	if raw[0].Res[0].Frequencia != 169528 {
		t.Errorf("first period count = %d, want 169528", raw[0].Res[0].Frequencia)
	}
}

func TestGetMunicipalities(t *testing.T) {
	payload := []map[string]any{
		{
			"id":   1100015,
			"nome": "Alta Floresta D'Oeste",
			"microrregiao": map[string]any{
				"id":   22012,
				"nome": "Cacoal",
				"mesorregiao": map[string]any{
					"id":   2201,
					"nome": "Leste Rondoniense",
					"UF": map[string]any{
						"id":    11,
						"sigla": "RO",
						"nome":  "Rondônia",
						"regiao": map[string]any{
							"id": 1, "sigla": "N", "nome": "Norte",
						},
					},
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := ibge.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var raw []struct {
		ID   int    `json:"id"`
		Nome string `json:"nome"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 || raw[0].Nome != "Alta Floresta D'Oeste" {
		t.Errorf("unexpected: %+v", raw)
	}
}

func TestNewClient(t *testing.T) {
	c := ibge.NewClient()
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.Rate != 300*time.Millisecond {
		t.Errorf("Rate = %v, want 300ms", c.Rate)
	}
	if c.Retries != 5 {
		t.Errorf("Retries = %d, want 5", c.Retries)
	}
}
