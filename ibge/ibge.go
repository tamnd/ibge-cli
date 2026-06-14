// Package ibge is the library behind the ibge command line:
// the HTTP client, request shaping, and the typed data models for the
// IBGE (Brazilian Institute of Geography and Statistics) public API at
// servicodados.ibge.gov.br. No API key is required.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
// Build your endpoint calls and JSON decoding on top of it.
package ibge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent identifies the client to the IBGE API. A real, honest
// User-Agent is both polite and the thing most likely to keep you unblocked.
const DefaultUserAgent = "ibge/dev (+https://github.com/tamnd/ibge-cli)"

// Host is the API host this client talks to.
const Host = "servicodados.ibge.gov.br"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// Config holds optional overrides a caller can inject before building a Client.
type Config struct {
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		UserAgent: DefaultUserAgent,
		Rate:      300 * time.Millisecond,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// Client talks to the IBGE API over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: a 30s timeout, a 300ms
// minimum gap between requests, and five retries on transient errors.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// get fetches url and decodes the JSON body into dst. It paces and retries
// according to the client's settings.
func (c *Client) get(ctx context.Context, url string, dst any) error {
	body, err := c.Get(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// ---------------------------------------------------------------------------
// Output types
// ---------------------------------------------------------------------------

// State represents a Brazilian state (unidade federativa).
type State struct {
	ID     int    `kit:"id" json:"id"`
	Code   string `json:"code"`   // sigla e.g. "SP"
	Name   string `json:"name"`   // nome e.g. "São Paulo"
	Region string `json:"region"` // regiao.nome e.g. "Sudeste"
}

// Region represents one of the 5 Brazilian macro-regions.
type Region struct {
	ID   int    `kit:"id" json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// Municipality represents a Brazilian municipality (city).
type Municipality struct {
	ID          int    `kit:"id" json:"id"`
	Name        string `json:"name"`
	State       string `json:"state"`       // UF.sigla
	StateName   string `json:"state_name"`  // UF.nome
	MicroRegion string `json:"micro_region"`
	MesoRegion  string `json:"meso_region"`
}

// NameFrequency holds the census frequency of a name in one decade.
type NameFrequency struct {
	Name   string `kit:"id" json:"name"`
	Period string `json:"period"`
	Count  int    `json:"count"`
}

// News is one IBGE news item.
type News struct {
	ID        int    `kit:"id" json:"id"`
	Title     string `json:"title"`
	Intro     string `json:"intro"`
	Published string `json:"published"` // data_publicacao
	Type      string `json:"type"`
}

// ---------------------------------------------------------------------------
// Wire decoders (raw IBGE shapes → our types)
// ---------------------------------------------------------------------------

type wireRegion struct {
	ID    int    `json:"id"`
	Sigla string `json:"sigla"`
	Nome  string `json:"nome"`
}

type wireUF struct {
	ID     int        `json:"id"`
	Sigla  string     `json:"sigla"`
	Nome   string     `json:"nome"`
	Regiao wireRegion `json:"regiao"`
}

type wireMeso struct {
	ID   int    `json:"id"`
	Nome string `json:"nome"`
	UF   wireUF `json:"UF"`
}

type wireMicro struct {
	ID         int      `json:"id"`
	Nome       string   `json:"nome"`
	Mesorregiao wireMeso `json:"mesorregiao"`
}

type wireMunicipio struct {
	ID           int       `json:"id"`
	Nome         string    `json:"nome"`
	Microrregiao wireMicro `json:"microrregiao"`
}

type wireNamePeriod struct {
	Periodo    string `json:"periodo"`
	Frequencia int    `json:"frequencia"`
}

type wireNameResult struct {
	Localidade string           `json:"localidade"`
	Res        []wireNamePeriod `json:"res"`
}

type wireNews struct {
	ID              int    `json:"id"`
	Tipo            string `json:"tipo"`
	Titulo          string `json:"titulo"`
	Introducao      string `json:"introducao"`
	DataPublicacao  string `json:"data_publicacao"`
}

// ---------------------------------------------------------------------------
// Client methods
// ---------------------------------------------------------------------------

// GetStates returns all 27 Brazilian states.
func (c *Client) GetStates(ctx context.Context) ([]*State, error) {
	var raw []wireUF
	if err := c.get(ctx, BaseURL+"/api/v1/localidades/estados", &raw); err != nil {
		return nil, err
	}
	out := make([]*State, len(raw))
	for i, r := range raw {
		out[i] = &State{
			ID:     r.ID,
			Code:   r.Sigla,
			Name:   r.Nome,
			Region: r.Regiao.Nome,
		}
	}
	return out, nil
}

// GetRegions returns the 5 Brazilian macro-regions.
func (c *Client) GetRegions(ctx context.Context) ([]*Region, error) {
	var raw []wireRegion
	if err := c.get(ctx, BaseURL+"/api/v1/localidades/regioes", &raw); err != nil {
		return nil, err
	}
	out := make([]*Region, len(raw))
	for i, r := range raw {
		out[i] = &Region{
			ID:   r.ID,
			Code: r.Sigla,
			Name: r.Nome,
		}
	}
	return out, nil
}

// GetMunicipalities returns municipalities, optionally filtered by state code.
// limit ≤ 0 means return all (uses the API's default pagination behaviour).
func (c *Client) GetMunicipalities(ctx context.Context, state string, limit int) ([]*Municipality, error) {
	var apiURL string
	if state != "" {
		apiURL = fmt.Sprintf("%s/api/v1/localidades/estados/%s/municipios", BaseURL, state)
	} else {
		apiURL = BaseURL + "/api/v1/localidades/municipios"
	}
	if limit > 0 {
		apiURL += fmt.Sprintf("?limit=%d", limit)
	}

	var raw []wireMunicipio
	if err := c.get(ctx, apiURL, &raw); err != nil {
		return nil, err
	}
	out := make([]*Municipality, len(raw))
	for i, r := range raw {
		uf := r.Microrregiao.Mesorregiao.UF
		out[i] = &Municipality{
			ID:          r.ID,
			Name:        r.Nome,
			State:       uf.Sigla,
			StateName:   uf.Nome,
			MicroRegion: r.Microrregiao.Nome,
			MesoRegion:  r.Microrregiao.Mesorregiao.Nome,
		}
	}
	return out, nil
}

// GetNameFrequency returns census frequency data for a given name, flattened
// to one NameFrequency record per decade.
func (c *Client) GetNameFrequency(ctx context.Context, name string) ([]*NameFrequency, error) {
	apiURL := fmt.Sprintf("%s/api/v2/censos/nomes/%s", BaseURL, name)
	var raw []wireNameResult
	if err := c.get(ctx, apiURL, &raw); err != nil {
		return nil, err
	}
	var out []*NameFrequency
	for _, r := range raw {
		for _, p := range r.Res {
			out = append(out, &NameFrequency{
				Name:   name,
				Period: p.Periodo,
				Count:  p.Frequencia,
			})
		}
	}
	return out, nil
}

// GetNews returns recent IBGE news items.
func (c *Client) GetNews(ctx context.Context, limit int) ([]*News, error) {
	if limit <= 0 {
		limit = 5
	}
	apiURL := fmt.Sprintf("%s/api/v3/noticias/?quantidade=%d", BaseURL, limit)
	var raw []wireNews
	if err := c.get(ctx, apiURL, &raw); err != nil {
		return nil, err
	}
	out := make([]*News, len(raw))
	for i, r := range raw {
		title := r.Titulo
		if title == "" {
			title = r.Introducao
			if len(title) > 100 {
				title = title[:100]
			}
		}
		out[i] = &News{
			ID:        r.ID,
			Title:     title,
			Intro:     r.Introducao,
			Published: r.DataPublicacao,
			Type:      r.Tipo,
		}
	}
	return out, nil
}
