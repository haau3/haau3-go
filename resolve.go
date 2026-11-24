package haau3

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/haau3/haau3-go/fhir"
)

// FHIR's RESTful address: a base, a resource type, an id, and a version after _history.
var (
	restfulAddress  = regexp.MustCompile(`^(https?://.+)/([A-Z][A-Za-z]+)/([A-Za-z0-9\-.]{1,64})(?:/_history/([A-Za-z0-9\-.]{1,64}))?$`)
	relativeAddress = regexp.MustCompile(`^([A-Z][A-Za-z]+)/([A-Za-z0-9\-.]{1,64})(?:/_history/([A-Za-z0-9\-.]{1,64}))?$`)
	absoluteAddress = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

type address struct{ base, resourceType, id, version string }

func restful(url string) (address, bool) {
	m := restfulAddress.FindStringSubmatch(url)
	if m == nil {
		return address{}, false
	}
	return address{base: m[1], resourceType: m[2], id: m[3], version: m[4]}, true
}

// Resolve is the resource a reference names, found in a Bundle as FHIR R4 resolves one ("Resolving
// references in Bundles"), with no request. T is the type the reference must name, a pointer such
// as *fhir.Practitioner, so the resource comes typed, or fhir.Resource for any type. A value type
// such as fhir.Practitioner does not compile: the Bundle holds pointers.
//
// from is the resource that holds the reference, or nil: one of the Bundle's entries, or a resource
// contained in one. A reference to a contained resource (#id) is found among the contained resources
// of the entry that holds from, or of from itself when it is not in the Bundle, and # alone is that
// resource. A relative reference (Practitioner/123) is resolved against the address of the entry
// that holds from, as FHIR says, so from must be in the Bundle. Without from, a relative reference is
// matched to the one entry whose address ends in that type and id. An absolute reference, a urn:uuid
// among them, is matched to the entry with that fullUrl, and a version (_history/2) to the entry's
// meta.versionId.
//
// It returns T's zero value and no error when the Bundle holds nothing the reference names, for a
// nil reference, one by identifier alone, and one FHIR gives no meaning. It returns an *Error of
// KindNotSent when two entries could each be the one it names, when it names a resource of
// another type than T, for a contained reference with no from, and for a relative reference from a
// resource that is not in the Bundle. No message repeats the reference. The resources are the
// Bundle's own, held as the SDK decodes them, each a pointer; nothing is copied or replaced.
//
//	for _, pharmacy := range haau3.Matches[*fhir.HealthcareService](page) {
//		organization, err := haau3.Resolve[*fhir.Organization](page, pharmacy.ProvidedBy, pharmacy)
//		if err != nil {
//			return err
//		}
//		if organization != nil && organization.Name != nil {
//			fmt.Println(*organization.Name)
//		}
//	}
func Resolve[T interface {
	fhir.Resource
	comparable
}](bundle *fhir.Bundle, reference *fhir.Reference, from fhir.Resource) (T, error) {
	var zero T
	found, err := resolveIn(bundle, referenceTarget(reference), from)
	if err != nil || found == nil {
		return zero, err
	}
	typed, ok := found.(T)
	if !ok {
		return zero, notSent(fmt.Sprintf("The reference names a resource of type %s, not %s.", found.ResourceType(), typeName[T]()))
	}
	return typed, nil
}

// Matches is the resources a search page matched, of type T, in page order, with no request: each
// entry whose search mode is "match", or says none. A resource an _include brought and a search's
// own OperationOutcome are not among them. T is a pointer such as *fhir.Slot, or fhir.Resource for
// every match; a value type such as fhir.Slot does not compile. Resources gives them across pages.
//
//	for _, slot := range haau3.Matches[*fhir.Slot](page) {
//		fmt.Println(*slot.Start)
//	}
func Matches[T interface {
	fhir.Resource
	comparable
}](page *fhir.Bundle) []T {
	if page == nil {
		return nil
	}
	var found []T
	for _, entry := range page.Entry {
		if !isMatch(entry) {
			continue
		}
		if resource, ok := entry.Resource.(T); ok {
			found = append(found, resource)
		}
	}
	return found
}

// isMatch reports whether an entry is one a search matched: it holds a resource, and its search mode
// is "match" or it says none. A typed nil (a nil *fhir.Slot) holds none.
func isMatch(entry fhir.BundleEntry) bool {
	if isNilResource(entry.Resource) {
		return false
	}
	return entry.Search == nil || entry.Search.Mode == nil || *entry.Search.Mode == "match"
}

func referenceTarget(reference *fhir.Reference) string {
	if reference == nil || reference.Reference == nil {
		return ""
	}
	return *reference.Reference
}

// typeName is the resource type T names: "Practitioner" for *fhir.Practitioner.
func typeName[T any]() string {
	t := reflect.TypeFor[T]()
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

// heldEntry is an entry that holds a resource; a deleted entry in a history holds none, and names
// nothing.
type heldEntry struct {
	fullURL  string
	resource fhir.Resource
}

func held(bundle *fhir.Bundle) []heldEntry {
	if bundle == nil {
		return nil
	}
	var out []heldEntry
	for _, entry := range bundle.Entry {
		if isNilResource(entry.Resource) {
			continue
		}
		url := ""
		if entry.FullURL != nil {
			url = *entry.FullURL
		}
		out = append(out, heldEntry{fullURL: url, resource: entry.Resource})
	}
	return out
}

// one is the one of found, or nil for none; two is a Bundle that does not say which it means.
func one(found []fhir.Resource) (fhir.Resource, error) {
	if len(found) > 1 {
		return nil, notSent(fmt.Sprintf("The reference matches %d entries of the Bundle, so which one it names is ambiguous.", len(found)))
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0], nil
}

// atAddress is the entry an absolute reference names: the one with that fullUrl, and with that
// version when it names one.
func atAddress(bundle *fhir.Bundle, reference string) (fhir.Resource, error) {
	url, version := reference, ""
	if a, ok := restful(reference); ok && a.version != "" {
		url, version = a.base+"/"+a.resourceType+"/"+a.id, a.version
	}
	var found []fhir.Resource
	for _, e := range held(bundle) {
		if e.fullURL == url && (version == "" || versionOf(e.resource) == version) {
			found = append(found, e.resource)
		}
	}
	return one(found)
}

func resolveIn(bundle *fhir.Bundle, target string, from fhir.Resource) (fhir.Resource, error) {
	// A reference by identifier alone names no entry; FHIR leaves finding one to the reader.
	if target == "" {
		return nil, nil
	}
	if strings.HasPrefix(target, "#") {
		if isNilResource(from) {
			return nil, notSent("A reference to a contained resource is found in the resource that holds it: pass from.")
		}
		// A contained resource's # references are to the resource that contains it, and to that
		// resource's other contained resources, as FHIR says; from outside the Bundle, to from's own.
		base := from
		if e, ok := holderOf(bundle, from); ok {
			base = e.resource
		}
		// # alone is the resource itself; #id is one it contains.
		if target == "#" {
			return base, nil
		}
		for _, contained := range containedOf(base) {
			if !isNilResource(contained) && idOf(contained) == target[1:] {
				return contained, nil
			}
		}
		return nil, nil
	}
	if absoluteAddress.MatchString(target) {
		return atAddress(bundle, target)
	}
	m := relativeAddress.FindStringSubmatch(target)
	// Anything else has no meaning FHIR defines.
	if m == nil {
		return nil, nil
	}
	resourceType, id, version := m[1], m[2], m[3]
	if !isNilResource(from) {
		holder, ok := holderOf(bundle, from)
		if !ok {
			return nil, notSent("The resource passed as from is not in this Bundle, as an entry or contained in one.")
		}
		// FHIR resolves a relative reference only against an entry at a RESTful address.
		a, ok := restful(holder.fullURL)
		if !ok {
			return nil, nil
		}
		return atAddress(bundle, a.base+"/"+target)
	}
	var found []fhir.Resource
	for _, e := range held(bundle) {
		a, ok := restful(e.fullURL)
		if ok && a.version == "" && a.resourceType == resourceType && a.id == id &&
			(version == "" || versionOf(e.resource) == version) {
			found = append(found, e.resource)
		}
	}
	return one(found)
}

// holderOf is the entry a resource's references resolve from: the entry that is the resource, or the
// one that contains it. Resources are told apart by pointer.
func holderOf(bundle *fhir.Bundle, r fhir.Resource) (heldEntry, bool) {
	entries := held(bundle)
	for _, e := range entries {
		if sameResource(e.resource, r) {
			return e, true
		}
	}
	for _, e := range entries {
		for _, contained := range containedOf(e.resource) {
			if sameResource(contained, r) {
				return e, true
			}
		}
	}
	return heldEntry{}, false
}

// sameResource reports whether a and b are the one resource: the same pointer. A resource held as a
// value has no identity, so it is the same as nothing, and comparing it never panics.
func sameResource(a, b fhir.Resource) bool {
	if !isPointer(a) || !isPointer(b) {
		return false
	}
	return reflect.TypeOf(a) == reflect.TypeOf(b) && pointerOf(a) == pointerOf(b)
}

func isPointer(r fhir.Resource) bool {
	return r != nil && reflect.ValueOf(r).Kind() == reflect.Pointer
}

func pointerOf(r fhir.Resource) uintptr { return reflect.ValueOf(r).Pointer() }

func isNilResource(r fhir.Resource) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// resourceFields is the struct behind a resource, or the zero Value for an OtherResource, which
// holds its JSON as it came.
func resourceFields(r fhir.Resource) reflect.Value {
	v := reflect.ValueOf(r)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return v
}

func stringField(v reflect.Value, name string) string {
	if !v.IsValid() {
		return ""
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Type() != reflect.TypeFor[*string]() || f.IsNil() {
		return ""
	}
	return *f.Interface().(*string)
}

func idOf(r fhir.Resource) string { return stringField(resourceFields(r), "ID") }

func versionOf(r fhir.Resource) string {
	v := resourceFields(r)
	if !v.IsValid() {
		return ""
	}
	meta := v.FieldByName("Meta")
	if !meta.IsValid() || meta.Type() != reflect.TypeFor[*fhir.Meta]() || meta.IsNil() {
		return ""
	}
	if m := meta.Interface().(*fhir.Meta); m.VersionID != nil {
		return *m.VersionID
	}
	return ""
}

func containedOf(r fhir.Resource) []fhir.Resource {
	v := resourceFields(r)
	if !v.IsValid() {
		return nil
	}
	f := v.FieldByName("Contained")
	if !f.IsValid() || f.Type() != reflect.TypeFor[[]fhir.Resource]() {
		return nil
	}
	return f.Interface().([]fhir.Resource)
}
