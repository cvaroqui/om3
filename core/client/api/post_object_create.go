package api

import (
	"opensvc.com/opensvc/core/client/request"
)

// PostObjectCreate are options supported by the api handler.
type PostObjectCreate struct {
	client         Poster                 `json:"-"`
	ObjectSelector string                 `json:"path,omitempty"`
	Namespace      string                 `json:"namespace,omitempty"`
	Template       string                 `json:"template,omitempty"`
	Provision      bool                   `json:"provision,omitempty"`
	Restore        bool                   `json:"restore,omitempty"`
	Data           map[string]interface{} `json:"data,omitempty"`
}

// NewPostObjectCreate allocates a PostObjectCreate struct and sets
// default values to its keys.
func NewPostObjectCreate(t Poster) *PostObjectCreate {
	return &PostObjectCreate{
		client: t,
		Data:   make(map[string]interface{}),
	}
}

// Do executes the request and returns the undecoded bytes.
func (o PostObjectCreate) Do() ([]byte, error) {
	req := request.NewFor("object_create", o)
	return o.client.Post(*req)
}