package httpapi

// The published API contract is generated at startup from the live route
// table: every registration made by Handler() is recorded and the served
// document contains exactly those routes (conditional native/local routes
// appear only when actually enabled). openapi.yaml supplies the detailed
// annotations — summaries, parameters, schemas — for known routes; a
// registered route without annotations still appears with a generic
// operation so the document can never omit a real endpoint. Routes that
// appear in the document but were never registered are impossible by
// construction.

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed openapi.yaml
var openapiSpec []byte

type apiRoute struct {
	Method  string
	Pattern string // mux pattern with the {path...} wildcard normalized for docs
}

// routeRegistry records every mux registration while Handler() builds it.
type routeRegistry struct{ routes []apiRoute }

func (rr *routeRegistry) record(pattern string) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		return
	}
	rr.routes = append(rr.routes, apiRoute{
		Method:  method,
		Pattern: strings.ReplaceAll(path, "{path...}", "{path}"),
	})
}

func isHTTPMethod(m string) bool {
	switch strings.ToUpper(m) {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		return true
	}
	return false
}

var (
	specOnce sync.Once
	specTop  map[string]any            // document root minus "paths"
	specOps  map[string]map[string]any // pattern -> METHOD -> operation
	specErr  error                     // spec parse failure (build-time file)
)

func loadSpec() error {
	specOnce.Do(func() {
		var doc map[string]any
		if specErr = yaml.Unmarshal(openapiSpec, &doc); specErr != nil {
			return
		}
		specOps = map[string]map[string]any{}
		if paths, ok := doc["paths"].(map[string]any); ok {
			for p, ops := range paths {
				if om, ok := ops.(map[string]any); ok {
					upper := map[string]any{}
					if parameters, ok := om["parameters"]; ok {
						upper["parameters"] = parameters
					}
					for m, op := range om {
						if isHTTPMethod(m) {
							upper[strings.ToUpper(m)] = op
						}
					}
					specOps[p] = upper
				}
			}
		}
		delete(doc, "paths")
		specTop = doc
	})
	return specErr
}

// orderedKV keeps path/method emission in registration order.
type orderedKV struct {
	Key   string
	Value any
}

type orderedPathOps []orderedKV // operations under one path

func (o orderedPathOps) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(kv.Key)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(kv.Value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func (o orderedPathOps) MarshalYAML() (any, error) {
	return mappingNode(o), nil
}

type orderedPaths []struct {
	Path string
	Ops  orderedPathOps
}

func (p orderedPaths) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, pe := range p {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(pe.Path)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(pe.Ops)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func (p orderedPaths) MarshalYAML() (any, error) {
	kvs := make([]orderedKV, len(p))
	for i, pe := range p {
		kvs[i] = orderedKV{Key: pe.Path, Value: pe.Ops}
	}
	return mappingNode(kvs), nil
}

// mappingNode renders an ordered YAML mapping (yaml.v3 dropped MapSlice).
func mappingNode(items []orderedKV) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, kv := range items {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: kv.Key},
			&yaml.Node{Kind: yaml.ScalarNode, Value: ""},
		)
		val := n.Content[len(n.Content)-1]
		if err := val.Encode(kv.Value); err != nil {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str",
				Value: "encoding error: " + err.Error()}
		}
	}
	return n
}

var docTags = []string{"agent", "memories", "documents", "folders", "attachments",
	"search", "graph", "events", "auth", "account", "vaults", "system"}

func guessTag(pattern string) string {
	for _, t := range docTags {
		if strings.Contains(pattern, "/"+t) || (t == "vaults" && strings.Contains(pattern, "/spaces")) {
			return t
		}
	}
	return "api"
}

// buildAPIDoc renders the contract from the recorded routes. The third
// return value lists routes that had no annotations (never expected in a
// healthy build; asserted empty by tests).
func buildAPIDoc(reg *routeRegistry) (jsonBody, yamlBody []byte, undocumented []string) {
	if err := loadSpec(); err != nil {
		return nil, nil, []string{"spec parse: " + err.Error()}
	}
	paths := orderedPaths{}
	index := map[string]int{}
	for _, rt := range reg.routes {
		op, ok := specOps[rt.Pattern][rt.Method]
		if !ok {
			undocumented = append(undocumented, rt.Method+" "+rt.Pattern)
			op = map[string]any{
				"summary": "Registered endpoint; annotations pending in openapi.yaml",
				"tags":    []string{guessTag(rt.Pattern)},
				"responses": map[string]any{
					"200": map[string]any{"description": "see implementation"},
					"4XX": map[string]any{"$ref": "#/components/responses/Error"},
				},
			}
		}
		if idx, seen := index[rt.Pattern]; seen {
			paths[idx].Ops = append(paths[idx].Ops, orderedKV{Key: strings.ToLower(rt.Method), Value: op})
			continue
		}
		index[rt.Pattern] = len(paths)
		ops := orderedPathOps{{Key: strings.ToLower(rt.Method), Value: op}}
		if common, ok := specOps[rt.Pattern]["parameters"]; ok {
			ops = append(orderedPathOps{{Key: "parameters", Value: common}}, ops...)
		}
		paths = append(paths, struct {
			Path string
			Ops  orderedPathOps
		}{Path: rt.Pattern, Ops: ops})
	}
	doc := map[string]any{"paths": paths}
	for k, v := range specTop {
		doc[k] = v
	}
	if jsonBody, err := json.MarshalIndent(doc, "", "  "); err != nil {
		return nil, nil, []string{"json: " + err.Error()}
	} else {
		if ym, err := yaml.Marshal(doc); err != nil {
			return jsonBody, nil, []string{"yaml: " + err.Error()}
		} else {
			return jsonBody, ym, undocumented
		}
	}
}

func (s *Server) handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	if len(s.apiDocJSON) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"unavailable","message":"api doc not built","retryable":true}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(s.apiDocJSON)
}

func (s *Server) handleOpenAPIYAML(w http.ResponseWriter, r *http.Request) {
	if len(s.apiDocYAML) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"unavailable","message":"api doc not built","retryable":true}}`))
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(s.apiDocYAML)
}
