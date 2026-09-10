// Package scim provides SCIM v2.0 schema definitions.
package scim

// SCIMSchema represents a SCIM schema definition.
type SCIMSchema struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Attributes  []SCIMAttribute `json:"attributes"`
}

// SCIMAttribute represents a SCIM schema attribute.
type SCIMAttribute struct {
	Name          string           `json:"name"`
	Type         string           `json:"type"`
	MultiValued  bool             `json:"multiValued,omitempty"`
	Required     bool             `json:"required,omitempty"`
	CaseExact    bool             `json:"caseExact,omitempty"`
	Mutability   string           `json:"mutability,omitempty"`
	Returned     string           `json:"returned,omitempty"`
	Uniqueness   string           `json:"uniqueness,omitempty"`
	ReferenceTypes []string       `json:"referenceTypes,omitempty"`
	SubAttributes []SCIMAttribute `json:"subAttributes,omitempty"`
}

// Standard schemas
var (
	// UserSchema is the SCIM 2.0 User schema.
	UserSchema = SCIMSchema{
		ID:          "urn:ietf:params:scim:schemas:core:2.0:User",
		Name:        "User",
		Description: "User Account",
		Attributes: []SCIMAttribute{
			{
				Name:        "userName",
				Type:        "string",
				Required:    true,
				Uniqueness:  "server",
				Mutability:  "readWrite",
				Returned:    "always",
			},
			{
				Name:       "name",
				Type:       "complex",
				Mutability: "readWrite",
				Returned:   "always",
				SubAttributes: []SCIMAttribute{
					{Name: "formatted", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "familyName", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "givenName", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "middleName", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "honorificPrefix", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "honorificSuffix", Type: "string", Mutability: "readWrite", Returned: "always"},
				},
			},
			{
				Name:        "displayName",
				Type:        "string",
				Mutability:  "readWrite",
				Returned:    "always",
			},
			{
				Name:        "emails",
				Type:        "complex",
				MultiValued: true,
				Mutability:  "readWrite",
				Returned:    "always",
				SubAttributes: []SCIMAttribute{
					{Name: "value", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "type", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "primary", Type: "boolean", Mutability: "readWrite", Returned: "always"},
				},
			},
			{
				Name:        "phoneNumbers",
				Type:        "complex",
				MultiValued: true,
				Mutability:  "readWrite",
				Returned:    "always",
				SubAttributes: []SCIMAttribute{
					{Name: "value", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "type", Type: "string", Mutability: "readWrite", Returned: "always"},
				},
			},
			{
				Name:        "active",
				Type:        "boolean",
				Mutability:  "readWrite",
				Returned:    "always",
			},
			{
				Name:        "externalId",
				Type:        "string",
				Mutability:  "readWrite",
				Returned:    "always",
			},
			{
				Name:        "meta",
				Type:        "complex",
				Mutability:  "readOnly",
				Returned:    "always",
				SubAttributes: []SCIMAttribute{
					{Name: "resourceType", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "created", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "lastModified", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "location", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "version", Type: "string", Mutability: "readOnly", Returned: "always"},
				},
			},
		},
	}

	// GroupSchema is the SCIM 2.0 Group schema.
	GroupSchema = SCIMSchema{
		ID:          "urn:ietf:params:scim:schemas:core:2.0:Group",
		Name:        "Group",
		Description: "Group",
		Attributes: []SCIMAttribute{
			{
				Name:        "displayName",
				Type:        "string",
				Required:    true,
				Mutability:  "readWrite",
				Returned:    "always",
			},
			{
				Name:        "members",
				Type:        "complex",
				MultiValued: true,
				Mutability:  "readWrite",
				Returned:    "always",
				SubAttributes: []SCIMAttribute{
					{Name: "value", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "$ref", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "type", Type: "string", Mutability: "readOnly", Returned: "always"},
				},
			},
			{
				Name:        "externalId",
				Type:        "string",
				Mutability:  "readWrite",
				Returned:    "always",
			},
			{
				Name:        "meta",
				Type:        "complex",
				Mutability:  "readOnly",
				Returned:    "always",
				SubAttributes: []SCIMAttribute{
					{Name: "resourceType", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "created", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "lastModified", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "location", Type: "string", Mutability: "readOnly", Returned: "always"},
					{Name: "version", Type: "string", Mutability: "readOnly", Returned: "always"},
				},
			},
		},
	}

	// EnterpriseUserSchema is the SCIM 2.0 Enterprise User extension schema.
	EnterpriseUserSchema = SCIMSchema{
		ID:          "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User",
		Name:        "EnterpriseUser",
		Description: "Enterprise User Extension",
		Attributes: []SCIMAttribute{
			{Name: "employeeNumber", Type: "string", Mutability: "readWrite", Returned: "always"},
			{Name: "costCenter", Type: "string", Mutability: "readWrite", Returned: "always"},
			{Name: "organization", Type: "string", Mutability: "readWrite", Returned: "always"},
			{Name: "division", Type: "string", Mutability: "readWrite", Returned: "always"},
			{Name: "department", Type: "string", Mutability: "readWrite", Returned: "always"},
			{Name: "manager", Type: "complex", Mutability: "readWrite", Returned: "always",
				SubAttributes: []SCIMAttribute{
					{Name: "value", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "$ref", Type: "string", Mutability: "readWrite", Returned: "always"},
					{Name: "displayName", Type: "string", Mutability: "readOnly", Returned: "always"},
				}},
		},
	}
)
