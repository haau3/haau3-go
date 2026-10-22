package main

import "testing"

func TestGoName(t *testing.T) {
	for in, want := range map[string]string{
		"readInsurancePlan":         "ReadInsurancePlan",
		"fhirMetadata":              "FHIRMetadata",
		"fhirBatch":                 "FHIRBatch",
		"codeSystemLookupPost":      "CodeSystemLookupPost",
		"checkInsurancePlanNetwork": "CheckInsurancePlanNetwork",
		"fullUrl":                   "FullURL",
		"id":                        "ID",
		"valueUuid":                 "ValueUUID",
		"implicitRules":             "ImplicitRules",
		"identifier":                "Identifier",
		"manifestUrl":               "ManifestURL",
		"scheduleId":                "ScheduleID",
		"div":                       "Div",
		"valueBase64Binary":         "ValueBase64Binary",
	} {
		if got := goName(in); got != want {
			t.Errorf("goName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocalName(t *testing.T) {
	for in, want := range map[string]string{"id": "id", "planId": "planID", "fhirVersion": "fhirVersion", "url": "url"} {
		if got := localName(in); got != want {
			t.Errorf("localName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParamName(t *testing.T) {
	for in, want := range map[string]string{
		"_count":                   "Count",
		"_id":                      "ID",
		"_revinclude":              "Revinclude",
		"name:contains":            "NameContains",
		"address-postalcode":       "AddressPostalcode",
		"new-patient-from-network": "NewPatientFromNetwork",
		"serviceTypeCode":          "ServiceTypeCode",
		"npi":                      "NPI",
		"url":                      "URL",
		"isVirtual":                "IsVirtual",
		// FHIR's chained search parameters, sent under their own names.
		"location.address-state":  "LocationAddressState",
		"organization.identifier": "OrganizationIdentifier",
	} {
		got, err := paramName(in)
		if err != nil || got != want {
			t.Errorf("paramName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"_", "9lives", "a/b"} {
		if got, err := paramName(bad); err == nil {
			t.Errorf("paramName(%q) = %q, want an error", bad, got)
		}
	}
}

func TestDocsURLsNumberRepeats(t *testing.T) {
	seen := docsURLs{}
	first := seen.url("Medical Benefits", "Read an insurance plan's network")
	if first != "https://docs.haau3.com/api-reference/medical-benefits/read-an-insurance-plans-network" {
		t.Fatal(first)
	}
	if again := seen.url("Medical Benefits", "Read an insurance plan's network"); again != first+"-1" {
		t.Fatal(again)
	}
}

func TestSingular(t *testing.T) {
	for in, want := range map[string]string{"serviceTypes": "serviceType", "specialties": "specialty", "identifier": "identifier"} {
		if got := singular(in); got != want {
			t.Errorf("singular(%q) = %q, want %q", in, got, want)
		}
	}
}
