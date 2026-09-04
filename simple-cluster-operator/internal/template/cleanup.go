package template

import (
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"text/template/parse"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

// Cleanup identity resolution preserves normal rendering whenever inputs are
// valid. Invalid provisioning inputs must not block identities that depend only
// on .SC, but must never be substituted with defaults or invalid raw values.
func cleanupInputs(scpc *showroomv1alpha1.SimpleClusterProviderConfig, sc *showroomv1alpha1.SimpleCluster, bodies ...string) (map[string]any, error) {
	inputs, err := buildInputs(scpc, sc)
	if err == nil {
		return inputs, nil
	}
	for _, body := range bodies {
		if !scOnlyTemplate(body) {
			return nil, fmt.Errorf("cleanup identity requires valid inputs: %w", err)
		}
	}
	return nil, nil
}

func targetNamespaceTemplate(scpc *showroomv1alpha1.SimpleClusterProviderConfig, resource *showroomv1alpha1.ResourceSpec) string {
	if resource.Namespace != "" {
		return resource.Namespace
	}
	return scpc.Spec.Target.Namespace
}

// ResolveNamespaceForCleanup permits an SC-only namespace despite unrelated
// invalid provisioning inputs. Input-dependent identities remain fail-closed.
func ResolveNamespaceForCleanup(scpc *showroomv1alpha1.SimpleClusterProviderConfig, sc *showroomv1alpha1.SimpleCluster, resource *showroomv1alpha1.ResourceSpec) (string, error) {
	raw := targetNamespaceTemplate(scpc, resource)
	inputs, err := cleanupInputs(scpc, sc, raw)
	if err != nil {
		return "", err
	}
	return renderNamespace(resource.Name, raw, scContext(sc), inputs)
}

// ResolveTargetForCleanup uses the same target name/namespace implementation as
// create, without requiring unrelated provisioning inputs to remain valid.
func ResolveTargetForCleanup(scpc *showroomv1alpha1.SimpleClusterProviderConfig, sc *showroomv1alpha1.SimpleCluster, resource *showroomv1alpha1.ResourceSpec, index int) (string, string, error) {
	inputs, err := cleanupInputs(scpc, sc, resource.NameTemplate, targetNamespaceTemplate(scpc, resource))
	if err != nil {
		return "", "", err
	}
	return resolveTarget(scpc, sc, resource, inputs, resource.Repeat != "", index)
}

// EvaluateRepeatForCleanup preserves full create semantics with valid inputs;
// on input failure it only accepts an SC-only count expression.
func EvaluateRepeatForCleanup(scpc *showroomv1alpha1.SimpleClusterProviderConfig, sc *showroomv1alpha1.SimpleCluster, resource *showroomv1alpha1.ResourceSpec) (int, error) {
	if _, err := buildInputs(scpc, sc); err == nil {
		return EvaluateRepeat(scpc, sc, resource, nil)
	}
	if _, err := cleanupInputs(scpc, sc, resource.Repeat); err != nil {
		return 0, err
	}
	raw, err := RenderString(resource.Name+".repeat", resource.Repeat, map[string]any{"SC": scContext(sc)})
	if err != nil {
		return 0, fmt.Errorf("render cleanup repeat: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("cleanup repeat %q must be a non-negative integer", raw)
	}
	return n, nil
}

// scOnlyTemplate conservatively proves independence from unavailable inputs.
// Merely rendering with an empty Inputs map is unsafe: index/get/default or
// serializing the whole context could silently manufacture another identity.
// Local variables, whole-dot/root access, named templates and unproven functions
// are intentionally rejected in this exceptional path. Random/time-dependent
// functions could otherwise invent a new identity and miss an unrecorded target.
// Valid-input rendering is unchanged.
func scOnlyTemplate(body string) bool {
	t, err := template.New("cleanup-identity").Funcs(renderFuncs()).Parse(body)
	if err != nil || len(t.Templates()) != 1 {
		return false
	}
	return scOnlyNode(t.Root)
}

func scOnlyNode(node parse.Node) bool {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return true
		}
		for _, child := range n.Nodes {
			if !scOnlyNode(child) {
				return false
			}
		}
	case *parse.ActionNode:
		return scOnlyNode(n.Pipe)
	case *parse.PipeNode:
		if len(n.Decl) != 0 {
			return false
		}
		for _, command := range n.Cmds {
			if !scOnlyNode(command) {
				return false
			}
		}
	case *parse.CommandNode:
		for _, argument := range n.Args {
			if !scOnlyNode(argument) {
				return false
			}
		}
	case *parse.FieldNode:
		return len(n.Ident) > 0 && n.Ident[0] == "SC"
	case *parse.VariableNode:
		return len(n.Ident) >= 2 && n.Ident[0] == "$" && n.Ident[1] == "SC"
	case *parse.IfNode:
		return scOnlyNode(n.Pipe) && scOnlyNode(n.List) && scOnlyNode(n.ElseList)
	case *parse.IdentifierNode:
		return deterministicCleanupFunction(n.Ident)
	case *parse.TextNode, *parse.StringNode, *parse.NumberNode, *parse.BoolNode, *parse.NilNode:
		return true
	default:
		return false
	}
	return true
}

// Keep this exceptional-path allowlist small and explicit. In particular, the
// full Sprig map contains randomness, clock reads and other external state.
func deterministicCleanupFunction(name string) bool {
	switch name {
	case "printf", "sha256sum", "trunc", "len", "and", "or", "not", "eq", "ne", "lt", "le", "gt", "ge":
		return true
	default:
		return false
	}
}
