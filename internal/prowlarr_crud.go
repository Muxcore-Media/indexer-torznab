package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	indexerv1 "github.com/Muxcore-Media/contracts-indexer/muxcore/indexer/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func normalizeIndexerImplementation(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "torznab":
		return "Torznab"
	case "newznab":
		return "Newznab"
	default:
		return strings.TrimSpace(raw)
	}
}

func implementationToken(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "newznab":
		return "newznab"
	default:
		return "torznab"
	}
}

func protocolForImplementation(impl string) string {
	if strings.EqualFold(impl, "Newznab") {
		return "usenet"
	}
	return "torrent"
}

func (c *prowlarrClient) doJSON(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	if c.base == "" {
		return nil, 0, fmt.Errorf("prowlarr is not configured")
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := doGuarded(c.http, req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

func (c *prowlarrClient) schemaByImplementation(ctx context.Context, impl string) (map[string]any, error) {
	raw, code, err := c.doJSON(ctx, http.MethodGet, "/api/v1/indexer/schema", nil)
	if err != nil {
		return nil, err
	}
	if code < 200 || code >= 300 {
		return nil, fmt.Errorf("prowlarr schema HTTP %d: %s", code, truncate(string(raw), 200))
	}
	var schemas []map[string]any
	if err := json.Unmarshal(raw, &schemas); err != nil {
		return nil, fmt.Errorf("prowlarr schema json: %w", err)
	}
	want := normalizeIndexerImplementation(impl)
	for _, schema := range schemas {
		if strings.EqualFold(fmt.Sprint(schema["implementation"]), want) {
			return schema, nil
		}
	}
	return nil, fmt.Errorf("prowlarr has no %s indexer definition", want)
}

func (c *prowlarrClient) getIndexerResource(ctx context.Context, id int32) (map[string]any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("indexer id is required")
	}
	raw, code, err := c.doJSON(ctx, http.MethodGet, "/api/v1/indexer/"+strconv.Itoa(int(id)), nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, status.Errorf(codes.NotFound, "indexer %d not found", id)
	}
	if code < 200 || code >= 300 {
		return nil, fmt.Errorf("prowlarr indexer HTTP %d: %s", code, truncate(string(raw), 200))
	}
	var resource map[string]any
	if err := json.Unmarshal(raw, &resource); err != nil {
		return nil, fmt.Errorf("prowlarr indexer json: %w", err)
	}
	return resource, nil
}

func setProwlarrField(resource map[string]any, name, value string) {
	fields, _ := resource["fields"].([]any)
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(fmt.Sprint(field["name"]), name) {
			field["value"] = value
			return
		}
	}
	resource["fields"] = append(fields, map[string]any{"name": name, "value": value})
}

func prowlarrFieldString(resource map[string]any, name string) string {
	fields, _ := resource["fields"].([]any)
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(fmt.Sprint(field["name"]), name) {
			return strings.TrimSpace(fmt.Sprint(field["value"]))
		}
	}
	return ""
}

func applyIndexerSpec(resource map[string]any, spec *indexerv1.IndexerSpec) {
	if name := strings.TrimSpace(spec.GetName()); name != "" {
		resource["name"] = name
	}
	resource["enable"] = spec.GetEnable()
	if url := strings.TrimSpace(spec.GetBaseUrl()); url != "" {
		setProwlarrField(resource, "baseUrl", url)
	}
	if key := strings.TrimSpace(spec.GetApiKey()); key != "" {
		setProwlarrField(resource, "apiKey", key)
	}
}

func specFromProwlarr(resource map[string]any) *indexerv1.IndexerSpec {
	if resource == nil {
		return &indexerv1.IndexerSpec{}
	}
	id, _ := resource["id"].(float64)
	impl := fmt.Sprint(resource["implementation"])
	key := prowlarrFieldString(resource, "apiKey")
	proto := strings.TrimSpace(fmt.Sprint(resource["protocol"]))
	if proto == "" {
		proto = protocolForImplementation(impl)
	}
	lang := strings.TrimSpace(fmt.Sprint(resource["language"]))
	if lang == "" {
		lang = "en"
	}
	enable, _ := resource["enable"].(bool)
	return &indexerv1.IndexerSpec{
		Id:             int32(id),
		Name:           strings.TrimSpace(fmt.Sprint(resource["name"])),
		Protocol:       proto,
		Implementation: implementationToken(impl),
		BaseUrl:        prowlarrFieldString(resource, "baseUrl"),
		Enable:         enable,
		HasApiKey:      key != "" && key != "<redacted>" && !strings.HasPrefix(key, "*"),
		Language:       lang,
	}
}

func (c *prowlarrClient) CreateIndexer(ctx context.Context, spec *indexerv1.IndexerSpec) (*indexerv1.IndexerSpec, error) {
	if spec == nil || strings.TrimSpace(spec.GetName()) == "" || strings.TrimSpace(spec.GetBaseUrl()) == "" {
		return nil, status.Error(codes.InvalidArgument, "name and base_url are required")
	}
	resource, err := c.schemaByImplementation(ctx, spec.GetImplementation())
	if err != nil {
		return nil, err
	}
	applyIndexerSpec(resource, spec)
	raw, code, err := c.doJSON(ctx, http.MethodPost, "/api/v1/indexer", resource)
	if err != nil {
		return nil, err
	}
	if code < 200 || code >= 300 {
		return nil, fmt.Errorf("prowlarr create HTTP %d: %s", code, truncate(string(raw), 200))
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil {
		return nil, fmt.Errorf("prowlarr create json: %w", err)
	}
	out := specFromProwlarr(created)
	out.ApiKey = ""
	return out, nil
}

func (c *prowlarrClient) UpdateIndexer(ctx context.Context, spec *indexerv1.IndexerSpec) (*indexerv1.IndexerSpec, error) {
	if spec == nil || spec.GetId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "indexer id is required")
	}
	resource, err := c.getIndexerResource(ctx, spec.GetId())
	if err != nil {
		return nil, err
	}
	applyIndexerSpec(resource, spec)
	raw, code, err := c.doJSON(ctx, http.MethodPut, "/api/v1/indexer/"+strconv.Itoa(int(spec.GetId())), resource)
	if err != nil {
		return nil, err
	}
	if code < 200 || code >= 300 {
		return nil, fmt.Errorf("prowlarr update HTTP %d: %s", code, truncate(string(raw), 200))
	}
	var updated map[string]any
	if err := json.Unmarshal(raw, &updated); err != nil {
		return nil, fmt.Errorf("prowlarr update json: %w", err)
	}
	out := specFromProwlarr(updated)
	out.ApiKey = ""
	return out, nil
}

func (c *prowlarrClient) DeleteIndexer(ctx context.Context, id int32) error {
	if id <= 0 {
		return status.Error(codes.InvalidArgument, "indexer id is required")
	}
	raw, code, err := c.doJSON(ctx, http.MethodDelete, "/api/v1/indexer/"+strconv.Itoa(int(id)), nil)
	if err != nil {
		return err
	}
	if code == http.StatusNotFound {
		return status.Errorf(codes.NotFound, "indexer %d not found", id)
	}
	if code < 200 || code >= 300 {
		return fmt.Errorf("prowlarr delete HTTP %d: %s", code, truncate(string(raw), 200))
	}
	return nil
}
