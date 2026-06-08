package bench

import (
	"encoding/json"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/go-openapi/spec"
	"github.com/wudi/jsonx"
)

func TestDecodeStructLarge(t *testing.T) {
	data := corpus["1_MB_10_Level_Formatted.json"]
	if data == nil {
		t.Fatal("corpus not loaded")
	}

	var v1 EmployeeData
	if err := jsonx.Unmarshal(data, &v1); err != nil {
		t.Fatalf("jsonx.Unmarshal failed: %v", err)
	}

	var v2 EmployeeData
	if err := json.Unmarshal(data, &v2); err != nil {
		t.Fatalf("stdlib json.Unmarshal failed: %v", err)
	}

	// Basic checks
	if len(v1.Employees) != len(v2.Employees) {
		t.Errorf("expected %d employees, got %d", len(v2.Employees), len(v1.Employees))
	}

	if len(v1.Employees) > 0 {
		e1 := v1.Employees[0]
		e2 := v2.Employees[0]
		if e1.ID != e2.ID {
			t.Errorf("expected ID %s, got %s", e2.ID, e1.ID)
		}
		if e1.Name != e2.Name {
			t.Errorf("expected Name %s, got %s", e2.Name, e1.Name)
		}
		if e1.Profile.Contact.Email != e2.Profile.Contact.Email {
			t.Errorf("expected Email %s, got %s", e2.Profile.Contact.Email, e1.Profile.Contact.Email)
		}
		if e1.Profile.Projects[0].Tasks[0].AssignedTo.Skills.Experience.Years != e2.Profile.Projects[0].Tasks[0].AssignedTo.Skills.Experience.Years {
			t.Errorf("expected Years %d, got %d", e2.Profile.Projects[0].Tasks[0].AssignedTo.Skills.Experience.Years, e1.Profile.Projects[0].Tasks[0].AssignedTo.Skills.Experience.Years)
		}
	}
}

func BenchmarkDecodeOpenAPISpec_Stdlib(b *testing.B) {
	for _, name := range []string{"api.github.com.json", "stripe_openapi_spec3.json"} {
		data := corpus[name]
		b.Run(name, func(b *testing.B) {
			runDecode(b, data, func(d []byte) error {
				var v spec.SwaggerProps

				return json.Unmarshal(d, &v)
			})
		})
	}
}

func BenchmarkDecodeOpenAPISpec_Jsonx(b *testing.B) {
	for _, name := range []string{"api.github.com.json", "stripe_openapi_spec3.json"} {
		data := corpus[name]
		b.Run(name, func(b *testing.B) {
			runDecode(b, data, func(d []byte) error {
				var v spec.SwaggerProps

				return jsonx.Unmarshal(d, &v)
			})
		})
	}
}
func BenchmarkDecodeOpenAPISpec_Sonic(b *testing.B) {
	for _, name := range []string{"api.github.com.json", "stripe_openapi_spec3.json"} {
		data := corpus[name]
		b.Run(name, func(b *testing.B) {
			runDecode(b, data, func(d []byte) error {
				var v spec.SwaggerProps

				return sonic.Unmarshal(d, &v)
			})
		})
	}
}
