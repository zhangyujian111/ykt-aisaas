// Package scim provides SCIM v2.0 server implementation for user/group provisioning.
package scim

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// SCIMServer implements SCIM v2.0 protocol.
type SCIMServer struct {
	userProvider  SCIMUserProvider
	groupProvider SCIMGroupProvider
	filterParser  *FilterParser
	authTokens    map[string]bool
	authMiddleware gin.HandlerFunc
}

// SCIMUser represents a SCIM 2.0 User resource.
type SCIMUser struct {
	Schemas     []string    `json:"schemas"`
	ID          string      `json:"id"`
	ExternalID  string      `json:"externalId,omitempty"`
	UserName    string      `json:"userName"`
	Name        SCIMName    `json:"name"`
	Emails      []SCIMEmail `json:"emails"`
	PhoneNumbers []SCIMPhone `json:"phoneNumbers,omitempty"`
	Active      bool        `json:"active"`
	Meta        SCIMMeta    `json:"meta,omitempty"`
}

// SCIMName represents the name component of a SCIM user.
type SCIMName struct {
	Formatted   string `json:"formatted,omitempty"`
	FamilyName  string `json:"familyName,omitempty"`
	GivenName   string `json:"givenName,omitempty"`
	MiddleName  string `json:"middleName,omitempty"`
	HonorificPrefix string `json:"honorificPrefix,omitempty"`
	HonorificSuffix string `json:"honorificSuffix,omitempty"`
}

// SCIMEmail represents an email address.
type SCIMEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// SCIMPhone represents a phone number.
type SCIMPhone struct {
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

// SCIMMeta contains metadata about a SCIM resource.
type SCIMMeta struct {
	ResourceType string `json:"resourceType,omitempty"`
	Created      string `json:"created,omitempty"`
	Modified     string `json:"lastModified,omitempty"`
	Location     string `json:"location,omitempty"`
	Version      string `json:"version,omitempty"`
}

// SCIMGroup represents a SCIM 2.0 Group resource.
type SCIMGroup struct {
	Schemas    []string       `json:"schemas"`
	ID         string         `json:"id"`
	ExternalID string         `json:"externalId,omitempty"`
	DisplayName string        `json:"displayName"`
	Members    []SCIMMember   `json:"members,omitempty"`
	Meta       SCIMMeta       `json:"meta,omitempty"`
}

// SCIMMember represents a member of a SCIM group.
type SCIMMember struct {
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
	Ref   string `json:"$ref,omitempty"`
}

// SCIMPatchOp represents a SCIM PATCH operation.
type SCIMPatchOp struct {
	Schemas []string       `json:"schemas"`
	Operations []SCIMOp    `json:"Operations"`
}

// SCIMOp represents a single SCIM PATCH operation.
type SCIMOp struct {
	Op    string          `json:"op"` // add, remove, replace
	Path  string          `json:"path,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

// SCIMListResponse represents a SCIM list response.
type SCIMListResponse struct {
	Schemas      []string          `json:"schemas"`
	TotalResults int               `json:"totalResults"`
	StartIndex   int               `json:"startIndex,omitempty"`
	ItemsPerPage int               `json:"itemsPerPage,omitempty"`
	Resources    []json.RawMessage `json:"Resources"`
}

// SCIMError represents a SCIM error response.
type SCIMError struct {
	Schemas []string `json:"schemas"`
	Status  int      `json:"status"`
	Detail  string   `json:"detail"`
}

// NewSCIMServer creates a new SCIM v2.0 server.
func NewSCIMServer(userProvider SCIMUserProvider, groupProvider SCIMGroupProvider, authTokens []string) *SCIMServer {
	tokenMap := make(map[string]bool)
	for _, t := range authTokens {
		tokenMap[t] = true
	}

	return &SCIMServer{
		userProvider:  userProvider,
		groupProvider: groupProvider,
		filterParser:  &FilterParser{},
		authTokens:    tokenMap,
		authMiddleware: func(c *gin.Context) {
			auth := c.GetHeader("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				c.AbortWithStatusJSON(http.StatusUnauthorized, SCIMError{
					Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
					Status:  401,
					Detail:  "Missing or invalid Authorization header",
				})
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if !tokenMap[token] {
				c.AbortWithStatusJSON(http.StatusUnauthorized, SCIMError{
					Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
					Status:  401,
					Detail:  "Invalid bearer token",
				})
				return
			}

			c.Next()
		},
	}
}

// AuthMiddleware returns the SCIM authentication middleware.
func (s *SCIMServer) AuthMiddleware() gin.HandlerFunc {
	return s.authMiddleware
}

// ListUsers handles GET /Users with filtering and pagination.
func (s *SCIMServer) ListUsers(c *gin.Context) {
	// Parse filter
	filter := c.Query("filter")
	var users []*SCIMUser
	var err error

	if filter != "" {
		users, err = s.userProvider.List(filter)
	} else {
		users, err = s.userProvider.List("")
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  500,
			Detail:  err.Error(),
		})
		return
	}

	// Pagination
	startIndex := parseInt(c.Query("startIndex"), 1)
	count := parseInt(c.Query("count"), 20)

	// Convert to []*SCIMUser for interface compatibility
	if users == nil {
		users = []*SCIMUser{}
	}

	totalResults := len(users)
	startIndex = max(1, startIndex)
	endIndex := min(startIndex+count-1, totalResults)

	var resources []json.RawMessage
	if startIndex <= endIndex {
		for _, u := range users[startIndex-1:endIndex] {
			data, _ := json.Marshal(u)
			resources = append(resources, data)
		}
	}

	response := SCIMListResponse{
		Schemas:      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		TotalResults: totalResults,
		StartIndex:   startIndex,
		ItemsPerPage: len(resources),
		Resources:    resources,
	}

	c.JSON(http.StatusOK, response)
}

// GetUser handles GET /Users/{id}.
func (s *SCIMServer) GetUser(c *gin.Context) {
	userID := c.Param("id")

	user, err := s.userProvider.GetByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  404,
			Detail:  fmt.Sprintf("User %s not found", userID),
		})
		return
	}

	c.JSON(http.StatusOK, user)
}

// CreateUser handles POST /Users.
func (s *SCIMServer) CreateUser(c *gin.Context) {
	var user SCIMUser
	if err := json.NewDecoder(c.Request.Body).Decode(&user); err != nil {
		c.JSON(http.StatusBadRequest, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  400,
			Detail:  "Invalid JSON body",
		})
		return
	}

	// Validate required fields
	if user.UserName == "" {
		c.JSON(http.StatusBadRequest, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  400,
			Detail:  "userName is required",
		})
		return
	}

	// Check if user already exists
	existing, _ := s.userProvider.GetByUserName(user.UserName)
	if existing != nil {
		c.JSON(http.StatusConflict, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  409,
			Detail:  fmt.Sprintf("User with userName %s already exists", user.UserName),
		})
		return
	}

	// Set schemas
	user.Schemas = []string{"urn:ietf:params:scim:schemas:core:2.0:User"}

	// Create user
	if err := s.userProvider.Create(&user); err != nil {
		c.JSON(http.StatusInternalServerError, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  500,
			Detail:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, user)
}

// ReplaceUser handles PUT /Users/{id}.
func (s *SCIMServer) ReplaceUser(c *gin.Context) {
	userID := c.Param("id")

	var user SCIMUser
	if err := json.NewDecoder(c.Request.Body).Decode(&user); err != nil {
		c.JSON(http.StatusBadRequest, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  400,
			Detail:  "Invalid JSON body",
		})
		return
	}

	// Validate user exists
	existing, _ := s.userProvider.GetByID(userID)
	if existing == nil {
		c.JSON(http.StatusNotFound, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  404,
			Detail:  fmt.Sprintf("User %s not found", userID),
		})
		return
	}

	user.Schemas = []string{"urn:ietf:params:scim:schemas:core:2.0:User"}

	if err := s.userProvider.Update(userID, &user); err != nil {
		c.JSON(http.StatusInternalServerError, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  500,
			Detail:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, user)
}

// PatchUser handles PATCH /Users/{id}.
func (s *SCIMServer) PatchUser(c *gin.Context) {
	userID := c.Param("id")

	var patch SCIMPatchOp
	if err := json.NewDecoder(c.Request.Body).Decode(&patch); err != nil {
		c.JSON(http.StatusBadRequest, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  400,
			Detail:  "Invalid JSON body",
		})
		return
	}

	if err := s.userProvider.ApplyPatch(userID, patch); err != nil {
		c.JSON(http.StatusInternalServerError, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  500,
			Detail:  err.Error(),
		})
		return
	}

	c.Status(http.StatusNoContent)
}

// DeleteUser handles DELETE /Users/{id}.
func (s *SCIMServer) DeleteUser(c *gin.Context) {
	userID := c.Param("id")

	if err := s.userProvider.Delete(userID); err != nil {
		c.JSON(http.StatusInternalServerError, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  500,
			Detail:  err.Error(),
		})
		return
	}

	c.Status(http.StatusNoContent)
}

// ListGroups handles GET /Groups.
func (s *SCIMServer) ListGroups(c *gin.Context) {
	filter := c.Query("filter")

	var groups []*SCIMGroup
	var err error

	if filter != "" {
		groups, err = s.groupProvider.List(filter)
	} else {
		groups, err = s.groupProvider.List("")
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  500,
			Detail:  err.Error(),
		})
		return
	}

	if groups == nil {
		groups = []*SCIMGroup{}
	}

	startIndex := parseInt(c.Query("startIndex"), 1)
	count := parseInt(c.Query("count"), 20)

	totalResults := len(groups)
	startIndex = max(1, startIndex)
	endIndex := min(startIndex+count-1, totalResults)

	var resources []json.RawMessage
	if startIndex <= endIndex {
		for _, g := range groups[startIndex-1:endIndex] {
			data, _ := json.Marshal(g)
			resources = append(resources, data)
		}
	}

	response := SCIMListResponse{
		Schemas:      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		TotalResults: totalResults,
		StartIndex:   startIndex,
		ItemsPerPage: len(resources),
		Resources:    resources,
	}

	c.JSON(http.StatusOK, response)
}

// GetGroup handles GET /Groups/{id}.
func (s *SCIMServer) GetGroup(c *gin.Context) {
	groupID := c.Param("id")

	group, err := s.groupProvider.GetByID(groupID)
	if err != nil {
		c.JSON(http.StatusNotFound, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  404,
			Detail:  fmt.Sprintf("Group %s not found", groupID),
		})
		return
	}

	c.JSON(http.StatusOK, group)
}

// CreateGroup handles POST /Groups.
func (s *SCIMServer) CreateGroup(c *gin.Context) {
	var group SCIMGroup
	if err := json.NewDecoder(c.Request.Body).Decode(&group); err != nil {
		c.JSON(http.StatusBadRequest, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  400,
			Detail:  "Invalid JSON body",
		})
		return
	}

	if group.DisplayName == "" {
		c.JSON(http.StatusBadRequest, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  400,
			Detail:  "displayName is required",
		})
		return
	}

	group.Schemas = []string{"urn:ietf:params:scim:schemas:core:2.0:Group"}

	if err := s.groupProvider.Create(&group); err != nil {
		c.JSON(http.StatusInternalServerError, SCIMError{
			Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
			Status:  500,
			Detail:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, group)
}

// GetSchemas handles GET /Schemas.
func (s *SCIMServer) GetSchemas(c *gin.Context) {
	schemas := []SCIMSchema{
		{
			ID:   "urn:ietf:params:scim:schemas:core:2.0:User",
			Name: "User",
			Attributes: []SCIMAttribute{
				{Name: "userName", Type: "string", Required: true, Uniqueness: "server"},
				{Name: "name", Type: "complex", SubAttributes: []SCIMAttribute{
					{Name: "formatted", Type: "string"},
					{Name: "familyName", Type: "string"},
					{Name: "givenName", Type: "string"},
					{Name: "middleName", Type: "string"},
					{Name: "honorificPrefix", Type: "string"},
					{Name: "honorificSuffix", Type: "string"},
				}},
				{Name: "displayName", Type: "string"},
				{Name: "emails", Type: "complex", MultiValued: true, SubAttributes: []SCIMAttribute{
					{Name: "value", Type: "string"},
					{Name: "type", Type: "string"},
					{Name: "primary", Type: "boolean"},
				}},
				{Name: "phoneNumbers", Type: "complex", MultiValued: true, SubAttributes: []SCIMAttribute{
					{Name: "value", Type: "string"},
					{Name: "type", Type: "string"},
				}},
				{Name: "active", Type: "boolean"},
				{Name: "externalId", Type: "string"},
			},
		},
		{
			ID:   "urn:ietf:params:scim:schemas:core:2.0:Group",
			Name: "Group",
			Attributes: []SCIMAttribute{
				{Name: "displayName", Type: "string", Required: true},
				{Name: "members", Type: "complex", MultiValued: true, SubAttributes: []SCIMAttribute{
					{Name: "value", Type: "string"},
					{Name: "$ref", Type: "string"},
					{Name: "type", Type: "string"},
				}},
				{Name: "externalId", Type: "string"},
			},
		},
	}

	c.JSON(http.StatusOK, map[string]any{
		"schemas":   []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": len(schemas),
		"Resources":  schemas,
	})
}

// ServiceProviderConfig handles GET /ServiceProviderConfig.
func (s *SCIMServer) ServiceProviderConfig(c *gin.Context) {
	config := map[string]any{
		"schemas":          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri": "https://docs.ykt.dev/scim",
		"patch": map[string]any{
			"supported": true,
		},
		"bulk": map[string]any{
			"supported":         false,
			"maxOperations":     0,
			"maxPayloadSize":    0,
		},
		"filter": map[string]any{
			"supported":       true,
			"maxResults":      100,
		},
		"changePassword": map[string]any{
			"supported": false,
		},
		"sort": map[string]any{
			"supported": false,
		},
		"etag": map[string]any{
			"supported": false,
		},
		"authenticationSchemes": []map[string]any{
			{
				"type":        "oauthbearertoken",
				"name":        "OAuth Bearer Token",
				"description": "Authentication scheme using the OAuth Bearer Token Standard",
				"specUri":     "https://www.rfc-editor.org/info/rfc6750",
			},
		},
	}

	c.JSON(http.StatusOK, config)
}

// parseInt parses an integer with a default value.
func parseInt(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return val
}

var _ = errors.New // avoid unused import
