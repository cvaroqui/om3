// Package daemonapi provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/deepmap/oapi-codegen version v1.11.0 DO NOT EDIT.
package daemonapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/deepmap/oapi-codegen/pkg/runtime"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
)

// ServerInterface represents all server handlers.
type ServerInterface interface {

	// (GET /daemon/running)
	GetDaemonRunning(w http.ResponseWriter, r *http.Request)

	// (GET /daemon/status)
	GetDaemonStatus(w http.ResponseWriter, r *http.Request, params GetDaemonStatusParams)

	// (POST /daemon/stop)
	PostDaemonStop(w http.ResponseWriter, r *http.Request)

	// (POST /node/monitor)
	PostNodeMonitor(w http.ResponseWriter, r *http.Request)

	// (GET /nodes/info)
	GetNodesInfo(w http.ResponseWriter, r *http.Request)

	// (POST /object/abort)
	PostObjectAbort(w http.ResponseWriter, r *http.Request)

	// (POST /object/clear)
	PostObjectClear(w http.ResponseWriter, r *http.Request)

	// (GET /object/config)
	GetObjectConfig(w http.ResponseWriter, r *http.Request, params GetObjectConfigParams)

	// (GET /object/file)
	GetObjectFile(w http.ResponseWriter, r *http.Request, params GetObjectFileParams)

	// (POST /object/monitor)
	PostObjectMonitor(w http.ResponseWriter, r *http.Request)

	// (GET /object/selector)
	GetObjectSelector(w http.ResponseWriter, r *http.Request, params GetObjectSelectorParams)

	// (POST /object/status)
	PostObjectStatus(w http.ResponseWriter, r *http.Request)

	// (GET /public/openapi)
	GetSwagger(w http.ResponseWriter, r *http.Request)
}

// ServerInterfaceWrapper converts contexts to parameters.
type ServerInterfaceWrapper struct {
	Handler            ServerInterface
	HandlerMiddlewares []MiddlewareFunc
	ErrorHandlerFunc   func(w http.ResponseWriter, r *http.Request, err error)
}

type MiddlewareFunc func(http.HandlerFunc) http.HandlerFunc

// GetDaemonRunning operation middleware
func (siw *ServerInterfaceWrapper) GetDaemonRunning(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetDaemonRunning(w, r)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// GetDaemonStatus operation middleware
func (siw *ServerInterfaceWrapper) GetDaemonStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var err error

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	// Parameter object where we will unmarshal all parameters from the context
	var params GetDaemonStatusParams

	// ------------- Optional query parameter "namespace" -------------
	if paramValue := r.URL.Query().Get("namespace"); paramValue != "" {

	}

	err = runtime.BindQueryParameter("form", true, false, "namespace", r.URL.Query(), &params.Namespace)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "namespace", Err: err})
		return
	}

	// ------------- Optional query parameter "relatives" -------------
	if paramValue := r.URL.Query().Get("relatives"); paramValue != "" {

	}

	err = runtime.BindQueryParameter("form", true, false, "relatives", r.URL.Query(), &params.Relatives)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "relatives", Err: err})
		return
	}

	// ------------- Optional query parameter "selector" -------------
	if paramValue := r.URL.Query().Get("selector"); paramValue != "" {

	}

	err = runtime.BindQueryParameter("form", true, false, "selector", r.URL.Query(), &params.Selector)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "selector", Err: err})
		return
	}

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetDaemonStatus(w, r, params)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// PostDaemonStop operation middleware
func (siw *ServerInterfaceWrapper) PostDaemonStop(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.PostDaemonStop(w, r)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// PostNodeMonitor operation middleware
func (siw *ServerInterfaceWrapper) PostNodeMonitor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.PostNodeMonitor(w, r)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// GetNodesInfo operation middleware
func (siw *ServerInterfaceWrapper) GetNodesInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetNodesInfo(w, r)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// PostObjectAbort operation middleware
func (siw *ServerInterfaceWrapper) PostObjectAbort(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.PostObjectAbort(w, r)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// PostObjectClear operation middleware
func (siw *ServerInterfaceWrapper) PostObjectClear(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.PostObjectClear(w, r)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// GetObjectConfig operation middleware
func (siw *ServerInterfaceWrapper) GetObjectConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var err error

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	// Parameter object where we will unmarshal all parameters from the context
	var params GetObjectConfigParams

	// ------------- Required query parameter "path" -------------
	if paramValue := r.URL.Query().Get("path"); paramValue != "" {

	} else {
		siw.ErrorHandlerFunc(w, r, &RequiredParamError{ParamName: "path"})
		return
	}

	err = runtime.BindQueryParameter("form", true, true, "path", r.URL.Query(), &params.Path)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "path", Err: err})
		return
	}

	// ------------- Optional query parameter "evaluate" -------------
	if paramValue := r.URL.Query().Get("evaluate"); paramValue != "" {

	}

	err = runtime.BindQueryParameter("form", true, false, "evaluate", r.URL.Query(), &params.Evaluate)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "evaluate", Err: err})
		return
	}

	// ------------- Optional query parameter "impersonate" -------------
	if paramValue := r.URL.Query().Get("impersonate"); paramValue != "" {

	}

	err = runtime.BindQueryParameter("form", true, false, "impersonate", r.URL.Query(), &params.Impersonate)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "impersonate", Err: err})
		return
	}

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetObjectConfig(w, r, params)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// GetObjectFile operation middleware
func (siw *ServerInterfaceWrapper) GetObjectFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var err error

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	// Parameter object where we will unmarshal all parameters from the context
	var params GetObjectFileParams

	// ------------- Required query parameter "path" -------------
	if paramValue := r.URL.Query().Get("path"); paramValue != "" {

	} else {
		siw.ErrorHandlerFunc(w, r, &RequiredParamError{ParamName: "path"})
		return
	}

	err = runtime.BindQueryParameter("form", true, true, "path", r.URL.Query(), &params.Path)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "path", Err: err})
		return
	}

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetObjectFile(w, r, params)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// PostObjectMonitor operation middleware
func (siw *ServerInterfaceWrapper) PostObjectMonitor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.PostObjectMonitor(w, r)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// GetObjectSelector operation middleware
func (siw *ServerInterfaceWrapper) GetObjectSelector(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var err error

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	// Parameter object where we will unmarshal all parameters from the context
	var params GetObjectSelectorParams

	// ------------- Required query parameter "selector" -------------
	if paramValue := r.URL.Query().Get("selector"); paramValue != "" {

	} else {
		siw.ErrorHandlerFunc(w, r, &RequiredParamError{ParamName: "selector"})
		return
	}

	err = runtime.BindQueryParameter("form", true, true, "selector", r.URL.Query(), &params.Selector)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "selector", Err: err})
		return
	}

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetObjectSelector(w, r, params)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// PostObjectStatus operation middleware
func (siw *ServerInterfaceWrapper) PostObjectStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx = context.WithValue(ctx, BasicAuthScopes, []string{""})

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.PostObjectStatus(w, r)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

// GetSwagger operation middleware
func (siw *ServerInterfaceWrapper) GetSwagger(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var handler = func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetSwagger(w, r)
	}

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler(w, r.WithContext(ctx))
}

type UnescapedCookieParamError struct {
	ParamName string
	Err       error
}

func (e *UnescapedCookieParamError) Error() string {
	return fmt.Sprintf("error unescaping cookie parameter '%s'", e.ParamName)
}

func (e *UnescapedCookieParamError) Unwrap() error {
	return e.Err
}

type UnmarshalingParamError struct {
	ParamName string
	Err       error
}

func (e *UnmarshalingParamError) Error() string {
	return fmt.Sprintf("Error unmarshaling parameter %s as JSON: %s", e.ParamName, e.Err.Error())
}

func (e *UnmarshalingParamError) Unwrap() error {
	return e.Err
}

type RequiredParamError struct {
	ParamName string
}

func (e *RequiredParamError) Error() string {
	return fmt.Sprintf("Query argument %s is required, but not found", e.ParamName)
}

type RequiredHeaderError struct {
	ParamName string
	Err       error
}

func (e *RequiredHeaderError) Error() string {
	return fmt.Sprintf("Header parameter %s is required, but not found", e.ParamName)
}

func (e *RequiredHeaderError) Unwrap() error {
	return e.Err
}

type InvalidParamFormatError struct {
	ParamName string
	Err       error
}

func (e *InvalidParamFormatError) Error() string {
	return fmt.Sprintf("Invalid format for parameter %s: %s", e.ParamName, e.Err.Error())
}

func (e *InvalidParamFormatError) Unwrap() error {
	return e.Err
}

type TooManyValuesForParamError struct {
	ParamName string
	Count     int
}

func (e *TooManyValuesForParamError) Error() string {
	return fmt.Sprintf("Expected one value for %s, got %d", e.ParamName, e.Count)
}

// Handler creates http.Handler with routing matching OpenAPI spec.
func Handler(si ServerInterface) http.Handler {
	return HandlerWithOptions(si, ChiServerOptions{})
}

type ChiServerOptions struct {
	BaseURL          string
	BaseRouter       chi.Router
	Middlewares      []MiddlewareFunc
	ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

// HandlerFromMux creates http.Handler with routing matching OpenAPI spec based on the provided mux.
func HandlerFromMux(si ServerInterface, r chi.Router) http.Handler {
	return HandlerWithOptions(si, ChiServerOptions{
		BaseRouter: r,
	})
}

func HandlerFromMuxWithBaseURL(si ServerInterface, r chi.Router, baseURL string) http.Handler {
	return HandlerWithOptions(si, ChiServerOptions{
		BaseURL:    baseURL,
		BaseRouter: r,
	})
}

// HandlerWithOptions creates http.Handler with additional options
func HandlerWithOptions(si ServerInterface, options ChiServerOptions) http.Handler {
	r := options.BaseRouter

	if r == nil {
		r = chi.NewRouter()
	}
	if options.ErrorHandlerFunc == nil {
		options.ErrorHandlerFunc = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	}
	wrapper := ServerInterfaceWrapper{
		Handler:            si,
		HandlerMiddlewares: options.Middlewares,
		ErrorHandlerFunc:   options.ErrorHandlerFunc,
	}

	r.Group(func(r chi.Router) {
		r.Get(options.BaseURL+"/daemon/running", wrapper.GetDaemonRunning)
	})
	r.Group(func(r chi.Router) {
		r.Get(options.BaseURL+"/daemon/status", wrapper.GetDaemonStatus)
	})
	r.Group(func(r chi.Router) {
		r.Post(options.BaseURL+"/daemon/stop", wrapper.PostDaemonStop)
	})
	r.Group(func(r chi.Router) {
		r.Post(options.BaseURL+"/node/monitor", wrapper.PostNodeMonitor)
	})
	r.Group(func(r chi.Router) {
		r.Get(options.BaseURL+"/nodes/info", wrapper.GetNodesInfo)
	})
	r.Group(func(r chi.Router) {
		r.Post(options.BaseURL+"/object/abort", wrapper.PostObjectAbort)
	})
	r.Group(func(r chi.Router) {
		r.Post(options.BaseURL+"/object/clear", wrapper.PostObjectClear)
	})
	r.Group(func(r chi.Router) {
		r.Get(options.BaseURL+"/object/config", wrapper.GetObjectConfig)
	})
	r.Group(func(r chi.Router) {
		r.Get(options.BaseURL+"/object/file", wrapper.GetObjectFile)
	})
	r.Group(func(r chi.Router) {
		r.Post(options.BaseURL+"/object/monitor", wrapper.PostObjectMonitor)
	})
	r.Group(func(r chi.Router) {
		r.Get(options.BaseURL+"/object/selector", wrapper.GetObjectSelector)
	})
	r.Group(func(r chi.Router) {
		r.Post(options.BaseURL+"/object/status", wrapper.PostObjectStatus)
	})
	r.Group(func(r chi.Router) {
		r.Get(options.BaseURL+"/public/openapi", wrapper.GetSwagger)
	})

	return r
}

// Base64 encoded, gzipped, json marshaled Swagger object
var swaggerSpec = []string{

	"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",
}

// GetSwagger returns the content of the embedded swagger specification file
// or error if failed to decode
func decodeSpec() ([]byte, error) {
	zipped, err := base64.StdEncoding.DecodeString(strings.Join(swaggerSpec, ""))
	if err != nil {
		return nil, fmt.Errorf("error base64 decoding spec: %s", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %s", err)
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(zr)
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %s", err)
	}

	return buf.Bytes(), nil
}

var rawSpec = decodeSpecCached()

// a naive cached of a decoded swagger spec
func decodeSpecCached() func() ([]byte, error) {
	data, err := decodeSpec()
	return func() ([]byte, error) {
		return data, err
	}
}

// Constructs a synthetic filesystem for resolving external references when loading openapi specifications.
func PathToRawSpec(pathToFile string) map[string]func() ([]byte, error) {
	var res = make(map[string]func() ([]byte, error))
	if len(pathToFile) > 0 {
		res[pathToFile] = rawSpec
	}

	return res
}

// GetSwagger returns the Swagger specification corresponding to the generated code
// in this file. The external references of Swagger specification are resolved.
// The logic of resolving external references is tightly connected to "import-mapping" feature.
// Externally referenced files must be embedded in the corresponding golang packages.
// Urls can be supported but this task was out of the scope.
func GetSwagger() (swagger *openapi3.T, err error) {
	var resolvePath = PathToRawSpec("")

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.ReadFromURIFunc = func(loader *openapi3.Loader, url *url.URL) ([]byte, error) {
		var pathToFile = url.String()
		pathToFile = path.Clean(pathToFile)
		getSpec, ok := resolvePath[pathToFile]
		if !ok {
			err1 := fmt.Errorf("path not found: %s", pathToFile)
			return nil, err1
		}
		return getSpec()
	}
	var specData []byte
	specData, err = rawSpec()
	if err != nil {
		return
	}
	swagger, err = loader.LoadFromData(specData)
	if err != nil {
		return
	}
	return
}
