package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Mastercard/terraform-provider-restapi/fakeserver"
	"github.com/Mastercard/terraform-provider-restapi/internal/apiclient"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cover the resource-layer reaction to a missing object added by
// PR #2: Read removes the resource from state, and ImportState returns an error.
//
// They invoke Read/ImportState directly (just as the apiclient-level
// TestReadObject404Handling invokes ReadObject directly), so the assertions land
// on the exact condition under test rather than a downstream symptom.

// newNotFoundTestResource wires a RestAPIObjectResource up to a running fakeserver
// holding the given objects.
func newNotFoundTestResource(t *testing.T, port int, objects map[string]map[string]interface{}) *RestAPIObjectResource {
	t.Helper()

	svr := fakeserver.NewFakeServer(port, objects, map[string]string{}, true, false, "")
	t.Cleanup(svr.Shutdown)

	opt := &apiclient.APIClientOpt{
		URI:         fmt.Sprintf("http://127.0.0.1:%d/", port),
		Timeout:     2,
		IDAttribute: "id",
	}

	return &RestAPIObjectResource{
		providerData: &ProviderData{opts: opt},
	}
}

// resourceSchema returns the restapi_object schema via the framework method.
func resourceSchema(t *testing.T, r *RestAPIObjectResource) resource.SchemaResponse {
	t.Helper()
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)
	require.False(t, resp.Diagnostics.HasError(), "building resource schema: %s", diagnosticsText(resp.Diagnostics))
	return *resp
}

// TestRestAPIObject_Read_RemovesMissingFromState verifies that Read drops the
// resource from state when the API reports the object is gone (404). The
// assertion is the direct effect of RemoveResource: resp.State must be null with
// no error diagnostics. Against the pre-fix code, Read instead tries to persist
// the empty API response and surfaces an error.
func TestRestAPIObject_Read_RemovesMissingFromState(t *testing.T) {
	// Server starts empty; "missing1" never exists.
	r := newNotFoundTestResource(t, 8150, map[string]map[string]interface{}{})
	schema := resourceSchema(t, r)

	// State as it would exist before a refresh: a managed object with id "missing1".
	model := RestAPIObjectResourceModel{
		Path:            types.StringValue("/api/objects"),
		ID:              types.StringValue("missing1"),
		Data:            jsontypes.NewNormalizedValue(`{"id":"missing1"}`),
		ForceNew:        types.ListNull(types.StringType),
		IgnoreChangesTo: types.ListNull(types.StringType),
		APIData:         types.MapNull(types.StringType),
	}

	reqState := tfsdk.State{Schema: schema.Schema}
	require.False(t, reqState.Set(context.Background(), &model).HasError(), "setting request state")

	req := resource.ReadRequest{State: reqState}
	resp := resource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}

	r.Read(context.Background(), req, &resp)

	assert.False(t, resp.Diagnostics.HasError(),
		"Read of a missing object must not error: %s", diagnosticsText(resp.Diagnostics))
	assert.True(t, resp.State.Raw.IsNull(),
		"missing object must be removed from state, got non-null state")
}

// TestRestAPIObject_Import_MissingObjectErrors verifies that importing an object
// that does not exist on the API fails with a clear "object not found" error
// instead of importing empty/garbage state.
func TestRestAPIObject_Import_MissingObjectErrors(t *testing.T) {
	// Server starts empty; nothing exists to import.
	r := newNotFoundTestResource(t, 8151, map[string]map[string]interface{}{})
	schema := resourceSchema(t, r)

	req := resource.ImportStateRequest{ID: "/api/objects/doesnotexist"}
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}}

	r.ImportState(context.Background(), req, &resp)

	assert.True(t, resp.Diagnostics.HasError(),
		"import of a missing object must produce an error: %s", diagnosticsText(resp.Diagnostics))
	assert.Contains(t, diagnosticsText(resp.Diagnostics), "object not found")
}

// diagnosticsText flattens Diagnostics into a single string for assertion messages.
func diagnosticsText(ds diag.Diagnostics) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteString(d.Summary())
		b.WriteString(": ")
		b.WriteString(d.Detail())
		b.WriteString("\n")
	}
	return b.String()
}
