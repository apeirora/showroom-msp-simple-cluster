package template

import (
	"testing"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func TestCleanupResolvesSCOnlyIdentityWithInvalidInputs(t *testing.T) {
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{{Name: "region", Type: showroomv1alpha1.InputTypeEnum, Default: "mdb", Values: []showroomv1alpha1.InputEnumValue{{Key: "mdb"}}}})
	scpc.Spec.Target.Namespace = "{{ .SC.namespace }}"
	sc := minimalSC(2, map[string]string{"region": "unsupported"})
	r := &showroomv1alpha1.ResourceSpec{Name: "claim", NameTemplate: `{{ printf "%s/%s" .SC.namespace .SC.name | sha256sum | trunc 20 | printf "sc-%s" }}`, Repeat: "{{ .SC.workers.nodes }}"}
	if _, err := ResolveInputs(scpc, sc); err == nil {
		t.Fatal("create input validation must remain strict")
	}
	name, namespace, err := ResolveTargetForCleanup(scpc, sc, r, 1)
	if err != nil || namespace != sc.Namespace || name == "" {
		t.Fatalf("SC-only cleanup identity failed: %s/%s %v", namespace, name, err)
	}
	valid := sc.DeepCopy()
	valid.Spec.Inputs["region"] = "mdb"
	wantName, wantNamespace, err := ResolveTarget(scpc, valid, r, 1)
	if err != nil || name != wantName || namespace != wantNamespace {
		t.Fatalf("cleanup diverged from create identity: %s/%s vs %s/%s (%v)", namespace, name, wantNamespace, wantName, err)
	}
	if count, err := EvaluateRepeatForCleanup(scpc, sc, r); err != nil || count != 2 {
		t.Fatalf("cleanup count=%d err=%v", count, err)
	}
}

func TestCleanupRejectsOpaqueOrInputDependentIdentity(t *testing.T) {
	for _, body := range []string{
		"{{ .Inputs.region }}",
		`{{ index .Inputs "region" | default "other" }}`,
		`{{ get .Inputs "region" | default "other" }}`,
		`{{ if hasKey .Inputs "region" }}one{{ else }}other{{ end }}`,
		`{{ index . "Inputs" "region" | default "other" }}`,
		`{{ get . "Inputs" | default "other" }}`,
		`{{ index $ "Inputs" "region" }}`,
		`{{ $root := . }}{{ $root.Inputs.region }}`,
		`{{ with .Inputs }}{{ .region }}{{ end }}`,
		`{{ define "alias" }}{{ .region }}{{ end }}{{ template "alias" .Inputs }}`,
		`{{ toJson . | sha256sum }}`,
	} {
		t.Run(body, func(t *testing.T) {
			if scOnlyTemplate(body) {
				t.Fatal("input-dependent or opaque identity was accepted")
			}
		})
	}
	for _, body := range []string{"literal", "{{ .SC.namespace }}", "{{ $.SC.name }}", `{{ if eq .SC.name "x" }}x{{ else }}y{{ end }}`} {
		if !scOnlyTemplate(body) {
			t.Fatalf("SC-only identity rejected: %s", body)
		}
	}
}

func TestCleanupRejectsUnprovenFunctionsWithInvalidInputs(t *testing.T) {
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{{Name: "region", Type: showroomv1alpha1.InputTypeEnum, Default: "mdb", Values: []showroomv1alpha1.InputEnumValue{{Key: "mdb"}}}})
	scpc.Spec.Target.Namespace = "{{ .SC.namespace }}"
	sc := minimalSC(1, map[string]string{"region": "unsupported"})
	for _, body := range []string{
		`{{ randAlphaNum 10 }}`,
		`{{ randNumeric 10 }}`,
		`{{ randInt 1 9 }}`,
		`{{ now | date "20060102" }}`,
		`{{ uuidv4 }}`,
		`{{ env "HOME" }}`,
		`{{ expandenv "$HOME" }}`,
		`{{ getHostByName "localhost" }}`,
		`{{ keys .SC }}`,
		`{{ if eq .SC.name "never" }}{{ randAlphaNum 10 }}{{ else }}literal{{ end }}`,
	} {
		t.Run(body, func(t *testing.T) {
			if scOnlyTemplate(body) {
				t.Fatal("unproven function was accepted for cleanup reconstruction")
			}
			r := &showroomv1alpha1.ResourceSpec{Name: "claim", NameTemplate: body}
			if name, _, err := ResolveTargetForCleanup(scpc, sc, r, 0); err == nil || name != "" {
				t.Fatalf("unproven cleanup identity rendered: name=%q err=%v", name, err)
			}
		})
	}
	valid := sc.DeepCopy()
	valid.Spec.Inputs["region"] = "mdb"
	r := &showroomv1alpha1.ResourceSpec{Name: "claim", NameTemplate: `{{ randAlphaNum 10 }}`}
	if name, _, err := ResolveTargetForCleanup(scpc, valid, r, 0); err != nil || len(name) != 10 {
		t.Fatalf("normal valid-input rendering changed: name=%q err=%v", name, err)
	}
}
