package template

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/iotemylabs/prepkit/schema"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// kitTemplateSchemaURL is the $id declared inside the embedded schema; the
// compiler resolves against the embedded bytes, never the network.
const kitTemplateSchemaURL = "https://prepkit.dev/schema/v0.1/kit-template.schema.json"

var (
	schemaOnce  sync.Once
	kitSchema   *jsonschema.Schema
	schemaError error
)

func compiledSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		c := jsonschema.NewCompiler()
		if err := c.AddResource(kitTemplateSchemaURL, bytes.NewReader(schema.KitTemplate)); err != nil {
			schemaError = err
			return
		}
		kitSchema, schemaError = c.Compile(kitTemplateSchemaURL)
	})
	return kitSchema, schemaError
}

// schemaCheck validates one template document against the kit-template JSON
// Schema and reports each leaf failure as its own error, formatted like
// validate.py: "<file>: <path>: <message>".
func schemaCheck(l *Loaded, r *Report) {
	sch, err := compiledSchema()
	if err != nil {
		// The embedded schema failing to compile is a defect in the binary
		// itself, not in the pack under validation.
		r.errf(l.File, "internal error compiling embedded schema: %v", err)
		return
	}

	// Round-trip through JSON so the instance uses exactly the value types
	// the validator expects, regardless of what the YAML decoder produced.
	b, err := json.Marshal(l.doc)
	if err != nil {
		r.errf(l.File, "document not representable as JSON: %v", err)
		return
	}
	var instance any
	if err := json.Unmarshal(b, &instance); err != nil {
		r.errf(l.File, "internal error re-reading document: %v", err)
		return
	}

	if err := sch.Validate(instance); err != nil {
		var ve *jsonschema.ValidationError
		if !errors.As(err, &ve) {
			r.errf(l.File, "schema validation: %v", err)
			return
		}
		ls := leaves(ve)
		sort.Slice(ls, func(i, j int) bool { return ls[i].InstanceLocation < ls[j].InstanceLocation })
		for _, leaf := range ls {
			r.errf(l.File, "%s: %s", displayPath(leaf.InstanceLocation), leaf.Message)
		}
	}
}

func leaves(ve *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(ve.Causes) == 0 {
		return []*jsonschema.ValidationError{ve}
	}
	var out []*jsonschema.ValidationError
	for _, c := range ve.Causes {
		out = append(out, leaves(c)...)
	}
	return out
}

func displayPath(instanceLocation string) string {
	p := strings.TrimPrefix(instanceLocation, "/")
	if p == "" {
		return "(root)"
	}
	return p
}
