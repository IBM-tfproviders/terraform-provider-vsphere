package vsphere

import (
	"log"
	"strings"
	"testing"

	"github.com/hashicorp/terraform/helper/schema"
)

type attributeProperty struct {
	value interface{}

	successCase bool
	expErr      string
	expWarn     string
}

type attributeValueValidationTestSpec struct {
	name        string
	validatorFn schema.SchemaValidateFunc
	values      []attributeProperty
}

func verifySchemaValidationFunctions(
	t *testing.T,
	validationCases []attributeValueValidationTestSpec) {

	for _, attr := range validationCases {

		log.Printf("* Executing validator function for attribute: '%s'", attr.name)

		for _, valObj := range attr.values {
			var warns []string
			var errors []error

			log.Printf("Validating:> Attribute name: '%s' value: '%v'",
				attr.name, valObj.value)

			warns, errors = attr.validatorFn(valObj.value, attr.name)
			// log.Printf("warns %s - errors %s", warns, errors)

			if valObj.successCase {
				if len(errors) > 0 || len(warns) > 0 {
					t.Fatalf("ParamValidationFailed: param '%s' value '%v' is not VALID.",
						attr.name, valObj.value)
				}

			} else {
				if errors != nil {
					ok := strings.Contains(errors[0].Error(), valObj.expErr)
					if !ok {
						t.Fatalf("ParamValidationFailed: '%s'. Expected ERROR '%v' not found.",
							attr.name, valObj.expErr)
					}
				} else if warns != nil {
					ok := strings.Contains(warns[0], valObj.expErr)
					if !ok {
						t.Fatalf("ParamValidationFailed: '%s'. Expected WARNING '%v' not found.",
							attr.name, valObj.expErr)
					}
				}
			}

		}
	}
}
