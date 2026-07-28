package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
)

const (
	maximumThreadPosts = 250
	stateField         = "state"
	ulidAlphabet       = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

var (
	// ErrInvalidJSON classifies input that is not one complete JSON value.
	ErrInvalidJSON = errors.New("invalid snapshot JSON")
	// ErrInvalidContract classifies a structurally invalid version 1 snapshot.
	ErrInvalidContract = errors.New("invalid snapshot contract")
	// ErrUnsupportedVersion classifies a well-typed schema version other than 1.
	ErrUnsupportedVersion = errors.New("unsupported snapshot version")

	utcTimestampPattern = regexp.MustCompile(
		`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}` +
			`(\.[0-9]+)?(Z|\+00:00)$`,
	)
)

// Error reports a value-free validation failure and preserves its classification and cause.
type Error struct {
	kind    error
	path    string
	problem string
	cause   error
}

// Error returns a stable diagnostic without echoing untrusted snapshot values.
func (validationError *Error) Error() string {
	return fmt.Sprintf("%s at %s: %s", validationError.kind, validationError.path, validationError.problem)
}

// Is exposes the validation classification to errors.Is.
func (validationError *Error) Is(target error) bool {
	return target == validationError.kind
}

// Unwrap preserves the standard-library parsing cause when one exists.
func (validationError *Error) Unwrap() error {
	return validationError.cause
}

// Parse validates one JSON document and returns its version 1 model without changing opaque objects.
func Parse(data []byte) (Snapshot, error) {
	raw, err := decodeDocument(data)
	if err != nil {
		return Snapshot{}, err
	}

	err = validateSnapshot(raw)
	if err != nil {
		return Snapshot{}, err
	}

	var parsed struct {
		LineageID  string `json:"lineageId"`
		ObservedAt string `json:"observedAt"`
		Boards     Boards `json:"boards"`
	}

	err = json.Unmarshal(raw, &parsed)
	if err != nil {
		return Snapshot{}, contractError("snapshot", "cannot decode validated document", err)
	}

	return Snapshot{
		SchemaVersion: Version,
		LineageID:     parsed.LineageID,
		ObservedAt:    parsed.ObservedAt,
		Boards:        parsed.Boards,
	}, nil
}

// Validate rejects a model whose JSON representation is not the exact version 1 contract.
func Validate(value Snapshot) error {
	_, err := Marshal(value)

	return err
}

// Marshal serializes a model only when the resulting document satisfies version 1 exactly.
func Marshal(value Snapshot) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, contractError("snapshot", "cannot encode model", err)
	}

	raw, err := decodeDocument(data)
	if err != nil {
		return nil, err
	}

	err = validateSnapshot(raw)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func decodeDocument(data []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))

	var raw json.RawMessage

	err := decoder.Decode(&raw)
	if err != nil {
		return nil, parseError("snapshot", "must be valid JSON", err)
	}

	var trailing json.RawMessage

	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}

		return nil, parseError("snapshot", "must contain one JSON value", err)
	}

	return raw, nil
}

func validateSnapshot(raw json.RawMessage) error {
	object, err := exactObject(raw, "snapshot", []string{"schemaVersion", "lineageId", "observedAt", "boards"}, nil)
	if err != nil {
		return err
	}

	version, err := integer(object["schemaVersion"], "snapshot.schemaVersion")
	if err != nil {
		return err
	}

	if version != Version {
		return versionError("snapshot.schemaVersion", "must equal 1")
	}

	lineageID, err := text(object["lineageId"], "snapshot.lineageId")
	if err != nil {
		return err
	}

	if !validULID(lineageID) {
		return contractError("snapshot.lineageId", "must be a valid ULID", nil)
	}

	observedAt, err := text(object["observedAt"], "snapshot.observedAt")
	if err != nil {
		return err
	}

	if !validObservedAt(observedAt) {
		return contractError("snapshot.observedAt", "must be a UTC RFC 3339 timestamp", nil)
	}

	return validateBoards(object["boards"], "snapshot.boards")
}

func validateBoards(raw json.RawMessage, path string) error {
	object, err := exactObject(raw, path, []string{stateField}, []string{"items"})
	if err != nil {
		return err
	}

	state, err := resourceState(object[stateField], path+".state")
	if err != nil {
		return err
	}

	switch state {
	case StateFailed:
		return forbidField(object, "items", path, "failed state must not contain items")
	case StatePresent:
		return validateBoardItems(object, path)
	case StateOversize:
		return contractError(path+".state", "must be failed or present", nil)
	default:
		return contractError(path+".state", "must be failed or present", nil)
	}
}

func validateBoardItems(object map[string]json.RawMessage, path string) error {
	items, exists := object["items"]
	if !exists {
		return contractError(path+".items", "is required for present state", nil)
	}

	values, err := array(items, path+".items")
	if err != nil {
		return err
	}

	for index, item := range values {
		err = validateBoardItem(item, indexed(path+".items", index))
		if err != nil {
			return err
		}
	}

	return nil
}

func validateBoardItem(raw json.RawMessage, path string) error {
	object, err := exactObject(raw, path, []string{"board"}, []string{"catalog"})
	if err != nil {
		return err
	}

	err = opaqueObject(object["board"], path+".board")
	if err != nil {
		return err
	}

	catalog, exists := object["catalog"]
	if !exists {
		return nil
	}

	return validateCatalog(catalog, path+".catalog")
}

func validateCatalog(raw json.RawMessage, path string) error {
	object, err := exactObject(raw, path, []string{stateField}, []string{"pages"})
	if err != nil {
		return err
	}

	state, err := resourceState(object[stateField], path+".state")
	if err != nil {
		return err
	}

	switch state {
	case StateFailed:
		return forbidField(object, "pages", path, "failed state must not contain pages")
	case StatePresent:
		return validateCatalogPages(object, path)
	case StateOversize:
		return contractError(path+".state", "must be failed or present", nil)
	default:
		return contractError(path+".state", "must be failed or present", nil)
	}
}

func validateCatalogPages(object map[string]json.RawMessage, path string) error {
	pages, exists := object["pages"]
	if !exists {
		return contractError(path+".pages", "is required for present state", nil)
	}

	values, err := array(pages, path+".pages")
	if err != nil {
		return err
	}

	threadCount := 0

	for index, page := range values {
		count, pageError := validatePage(page, indexed(path+".pages", index))
		if pageError != nil {
			return pageError
		}

		threadCount += count

		if threadCount > MaximumCatalogThreads {
			return contractError(path+".pages", "must contain at most 250 threads", nil)
		}
	}

	return nil
}

func validatePage(raw json.RawMessage, path string) (int, error) {
	object, err := exactObject(raw, path, []string{"metadata", "threads"}, nil)
	if err != nil {
		return 0, err
	}

	err = opaqueObject(object["metadata"], path+".metadata")
	if err != nil {
		return 0, err
	}

	threads, err := array(object["threads"], path+".threads")
	if err != nil {
		return 0, err
	}

	for index, thread := range threads {
		err = validateThreadEntry(thread, indexed(path+".threads", index))
		if err != nil {
			return 0, err
		}
	}

	return len(threads), nil
}

func validateThreadEntry(raw json.RawMessage, path string) error {
	object, err := exactObject(raw, path, []string{"summary"}, []string{"thread"})
	if err != nil {
		return err
	}

	err = opaqueObject(object["summary"], path+".summary")
	if err != nil {
		return err
	}

	thread, exists := object["thread"]
	if !exists {
		return nil
	}

	return validateThread(thread, path+".thread")
}

func validateThread(raw json.RawMessage, path string) error {
	object, err := exactObject(raw, path, []string{stateField}, []string{"posts"})
	if err != nil {
		return err
	}

	state, err := resourceState(object[stateField], path+".state")
	if err != nil {
		return err
	}

	switch state {
	case StateFailed:
		return forbidField(object, "posts", path, "failed state must not contain posts")
	case StatePresent:
		return validateThreadPosts(object, path, false)
	case StateOversize:
		return validateThreadPosts(object, path, true)
	default:
		return contractError(path+".state", "must be failed, present, or oversize", nil)
	}
}

func validateThreadPosts(object map[string]json.RawMessage, path string, oversize bool) error {
	posts, exists := object["posts"]
	if !exists {
		return contractError(path+".posts", "is required for present and oversize states", nil)
	}

	values, err := array(posts, path+".posts")
	if err != nil {
		return err
	}

	if !oversize && len(values) > maximumThreadPosts {
		return contractError(path+".posts", "present state must contain at most 250 posts", nil)
	}

	if oversize && len(values) != maximumThreadPosts {
		return contractError(path+".posts", "oversize state must contain exactly 250 posts", nil)
	}

	for index, post := range values {
		err = opaqueObject(post, indexed(path+".posts", index))
		if err != nil {
			return err
		}
	}

	return nil
}

func forbidField(object map[string]json.RawMessage, field, path, problem string) error {
	if _, exists := object[field]; exists {
		return contractError(path, problem, nil)
	}

	return nil
}

func exactObject(
	raw json.RawMessage,
	path string,
	required, optional []string,
) (map[string]json.RawMessage, error) {
	if !isObject(raw) {
		return nil, contractError(path, "must be an object", nil)
	}

	var object map[string]json.RawMessage

	err := json.Unmarshal(raw, &object)
	if err != nil {
		return nil, contractError(path, "must be an object", err)
	}

	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, field := range required {
		allowed[field] = struct{}{}
		if _, exists := object[field]; !exists {
			return nil, contractError(path+"."+field, "is required", nil)
		}
	}

	for _, field := range optional {
		allowed[field] = struct{}{}
	}

	for field := range object {
		if _, exists := allowed[field]; !exists {
			return nil, contractError(path, "contains an unknown field", nil)
		}
	}

	return object, nil
}

func array(raw json.RawMessage, path string) ([]json.RawMessage, error) {
	if firstByte(raw) != '[' {
		return nil, contractError(path, "must be an array", nil)
	}

	var values []json.RawMessage

	err := json.Unmarshal(raw, &values)
	if err != nil {
		return nil, contractError(path, "must be an array", err)
	}

	return values, nil
}

func text(raw json.RawMessage, path string) (string, error) {
	if firstByte(raw) != '"' {
		return "", contractError(path, "must be a string", nil)
	}

	var value string

	err := json.Unmarshal(raw, &value)
	if err != nil {
		return "", contractError(path, "must be a string", err)
	}

	return value, nil
}

func integer(raw json.RawMessage, path string) (float64, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, contractError(path, "must be an integer", nil)
	}

	var value float64

	err := json.Unmarshal(raw, &value)
	if err != nil {
		return 0, contractError(path, "must be an integer", err)
	}

	if math.Trunc(value) != value {
		return 0, contractError(path, "must be an integer", nil)
	}

	return value, nil
}

func resourceState(raw json.RawMessage, path string) (State, error) {
	value, err := text(raw, path)
	if err != nil {
		return "", err
	}

	return State(value), nil
}

func opaqueObject(raw json.RawMessage, path string) error {
	if !isObject(raw) {
		return contractError(path, "must be an opaque object", nil)
	}

	return nil
}

func isObject(raw json.RawMessage) bool {
	return firstByte(raw) == '{'
}

func firstByte(raw json.RawMessage) byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 0
	}

	return trimmed[0]
}

func validULID(value string) bool {
	if len(value) != 26 || value[0] < '0' || value[0] > '7' {
		return false
	}

	for index := 1; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' {
			character -= 'a' - 'A'
		}

		if !strings.ContainsRune(ulidAlphabet, rune(character)) {
			return false
		}
	}

	return true
}

func validObservedAt(value string) bool {
	if !utcTimestampPattern.MatchString(value) {
		return false
	}

	_, err := time.Parse(time.RFC3339, value[:19]+"Z")

	return err == nil
}

func indexed(path string, index int) string {
	return fmt.Sprintf("%s[%d]", path, index)
}

func parseError(path, problem string, cause error) error {
	return &Error{kind: ErrInvalidJSON, path: path, problem: problem, cause: cause}
}

func contractError(path, problem string, cause error) error {
	return &Error{kind: ErrInvalidContract, path: path, problem: problem, cause: cause}
}

func versionError(path, problem string) error {
	return &Error{kind: ErrUnsupportedVersion, path: path, problem: problem}
}
