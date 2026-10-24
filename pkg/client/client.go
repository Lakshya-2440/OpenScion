package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/queue"
	"github.com/aether/scn/pkg/tree"
)

type authTransport struct {
	token string
	base  http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	transport  *authTransport
}

func NewClient(serverURL string) *Client {
	serverURL = strings.TrimSuffix(serverURL, "/")
	token := os.Getenv("SCION_AUTH_TOKEN")
	if token == "" {
		token = os.Getenv("AETHER_AUTH_TOKEN")
	}

	transport := &authTransport{
		token: token,
		base:  http.DefaultTransport,
	}

	return &Client{
		baseURL:   serverURL,
		transport: transport,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

func (c *Client) SetAuthToken(token string) {
	c.transport.token = token
}

type RepoInfoResponse struct {
	Repo           db.Repository    `json:"repo"`
	Branch         db.Branch        `json:"branch"`
	HeadChangeset  *graph.Changeset `json:"head_changeset"`
	SequenceNumber int64            `json:"sequence_number"`
}

type FileQueryResponse struct {
	Path        string `json:"path"`
	Hash        string `json:"hash"`
	Size        int64  `json:"size"`
	Mode        uint32 `json:"mode"`
	ContentText string `json:"content_text"`
}

type BlameLine struct {
	LineNumber int    `json:"line_number"`
	Author     string `json:"author"`
	AgentModel string `json:"agent_model,omitempty"`
	CommitID   string `json:"commit_id"`
	CommitSeq  int64  `json:"sequence_number"`
	Content    string `json:"content"`
}

type BlameResponse struct {
	Path  string      `json:"path"`
	Lines []BlameLine `json:"lines"`
}

func (c *Client) CreateRepo(name string) (*db.Repository, error) {
	body, _ := json.Marshal(map[string]string{"name": name})
	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/repos/create", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, string(errResp))
	}

	var r db.Repository
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) GetRepoInfo(name string) (*RepoInfoResponse, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/api/v1/repos/%s/info", c.baseURL, url.PathEscape(name)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("repo info failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var info RepoInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

type chunkWire struct {
	Hash           string `json:"hash"`
	UncompressedSz int    `json:"uncompressed_size"`
	DataBase64     string `json:"data_base64"`
}

func (c *Client) UploadChunks(chunks []*cas.BlobChunk) error {
	if len(chunks) == 0 {
		return nil
	}

	const maxBatchBytes = 8 * 1024 * 1024 // 8MB per HTTP request
	const maxBatchCount = 64

	for i := 0; i < len(chunks); {
		var batch []chunkWire
		batchBytes := 0

		for i < len(chunks) && len(batch) < maxBatchCount && (batchBytes == 0 || batchBytes+len(chunks[i].Data) <= maxBatchBytes) {
			batch = append(batch, chunkWire{
				Hash:           chunks[i].Hash,
				UncompressedSz: chunks[i].UncompressedSz,
				DataBase64:     base64.StdEncoding.EncodeToString(chunks[i].Data),
			})
			batchBytes += len(chunks[i].Data)
			i++
		}

		body, err := json.Marshal(batch)
		if err != nil {
			return err
		}

		resp, err := c.httpClient.Post(c.baseURL+"/api/v1/cas/blobs/batch-put", "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			errResp, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("upload chunks failed (%d): %s", resp.StatusCode, string(errResp))
		}
		resp.Body.Close()
	}

	return nil
}

func (c *Client) DownloadChunks(hashes []string) ([]*cas.BlobChunk, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	const batchSize = 64
	var allChunks []*cas.BlobChunk

	for i := 0; i < len(hashes); i += batchSize {
		end := i + batchSize
		if end > len(hashes) {
			end = len(hashes)
		}
		batch := hashes[i:end]

		reqBody, _ := json.Marshal(map[string][]string{"hashes": batch})
		resp, err := c.httpClient.Post(c.baseURL+"/api/v1/cas/blobs/batch-get", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			errResp, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("download chunks failed (%d): %s", resp.StatusCode, string(errResp))
		}

		var wireList []chunkWire
		decodeErr := json.NewDecoder(resp.Body).Decode(&wireList)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		expected := make(map[string]bool, len(batch))
		for _, hash := range batch {
			expected[hash] = true
		}
		for _, wire := range wireList {
			if !expected[wire.Hash] {
				return nil, fmt.Errorf("server returned unexpected chunk %s", wire.Hash)
			}
			raw, err := base64.StdEncoding.DecodeString(wire.DataBase64)
			if err != nil {
				return nil, fmt.Errorf("invalid chunk %s from server: %w", wire.Hash, err)
			}
			if err := cas.ValidateChunk(&cas.BlobChunk{Hash: wire.Hash, UncompressedSz: wire.UncompressedSz, Data: raw}); err != nil {
				return nil, fmt.Errorf("invalid chunk %s from server: %w", wire.Hash, err)
			}
			delete(expected, wire.Hash)
		}
		if len(expected) != 0 {
			return nil, fmt.Errorf("server omitted %d requested chunks", len(expected))
		}

		for _, w := range wireList {
			raw, err := base64.StdEncoding.DecodeString(w.DataBase64)
			if err != nil {
				continue
			}
			allChunks = append(allChunks, &cas.BlobChunk{
				Hash:           w.Hash,
				UncompressedSz: w.UncompressedSz,
				Data:           raw,
			})
		}
	}

	return allChunks, nil
}

func (c *Client) UploadManifests(manifests []*cas.BlobManifest) error {
	if len(manifests) == 0 {
		return nil
	}

	const batchSize = 128
	for i := 0; i < len(manifests); i += batchSize {
		end := i + batchSize
		if end > len(manifests) {
			end = len(manifests)
		}
		batch := manifests[i:end]

		body, err := json.Marshal(batch)
		if err != nil {
			return err
		}

		resp, err := c.httpClient.Post(c.baseURL+"/api/v1/cas/manifests/batch-put", "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			errResp, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("upload manifests failed (%d): %s", resp.StatusCode, string(errResp))
		}
		resp.Body.Close()
	}

	return nil
}

func (c *Client) DownloadManifests(hashes []string) ([]*cas.BlobManifest, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	const batchSize = 128
	var allManifests []*cas.BlobManifest

	for i := 0; i < len(hashes); i += batchSize {
		end := i + batchSize
		if end > len(hashes) {
			end = len(hashes)
		}
		batch := hashes[i:end]

		reqBody, _ := json.Marshal(map[string][]string{"hashes": batch})
		resp, err := c.httpClient.Post(c.baseURL+"/api/v1/cas/manifests/batch-get", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			errResp, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("download manifests failed (%d): %s", resp.StatusCode, string(errResp))
		}

		var manifests []*cas.BlobManifest
		decodeErr := json.NewDecoder(resp.Body).Decode(&manifests)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		expected := make(map[string]bool, len(batch))
		for _, hash := range batch {
			expected[hash] = true
		}
		for _, manifest := range manifests {
			if manifest == nil || !expected[manifest.BlobHash] {
				return nil, fmt.Errorf("server returned unexpected manifest")
			}
			if err := cas.ValidateManifest(manifest); err != nil {
				return nil, fmt.Errorf("invalid manifest %s from server: %w", manifest.BlobHash, err)
			}
			delete(expected, manifest.BlobHash)
		}
		if len(expected) != 0 {
			return nil, fmt.Errorf("server omitted %d requested manifests", len(expected))
		}

		allManifests = append(allManifests, manifests...)
	}

	return allManifests, nil
}

func (c *Client) SaveTree(t *tree.Tree) error {
	body, err := json.Marshal(t)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/cas/trees/put", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("save tree failed (%d): %s", resp.StatusCode, string(errResp))
	}
	return nil
}

func (c *Client) GetTree(hash string) (*tree.Tree, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/api/v1/cas/trees/%s", c.baseURL, hash))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get tree failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var t tree.Tree
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) GetSparseTree(hash string, patterns []string) (*tree.Tree, error) {
	patParam := strings.Join(patterns, ",")
	u := fmt.Sprintf("%s/api/v1/cas/trees/%s/sparse?patterns=%s", c.baseURL, hash, url.QueryEscape(patParam))
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get sparse tree failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var t tree.Tree
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) SubmitChangeset(repoID string, cs *graph.Changeset) (*queue.SubmitResult, error) {
	payload := map[string]any{
		"repo_id":   repoID,
		"changeset": cs,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/changeset/submit", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("submit failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var res queue.SubmitResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) QueryLog(repoName string, limit int) ([]graph.Changeset, error) {
	u := fmt.Sprintf("%s/api/v1/query/log?repo=%s&limit=%d", c.baseURL, url.QueryEscape(repoName), limit)
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query log failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var list []graph.Changeset
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

func (c *Client) QueryChangeset(id string) (*graph.Changeset, error) {
	u := fmt.Sprintf("%s/api/v1/query/changeset?id=%s", c.baseURL, url.QueryEscape(id))
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query changeset failed (%d): %s", resp.StatusCode, string(errResp))
	}
	var cs graph.Changeset
	if err := json.NewDecoder(resp.Body).Decode(&cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

func (c *Client) QueryFile(repoName, path, rev string) (*FileQueryResponse, error) {
	u := fmt.Sprintf("%s/api/v1/query/file?repo=%s&path=%s&rev=%s",
		c.baseURL, url.QueryEscape(repoName), url.QueryEscape(path), url.QueryEscape(rev))
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query file failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var f FileQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (c *Client) QueryBlame(repoName, path string) (*BlameResponse, error) {
	u := fmt.Sprintf("%s/api/v1/query/blame?repo=%s&path=%s", c.baseURL, url.QueryEscape(repoName), url.QueryEscape(path))
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query blame failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var b BlameResponse
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (c *Client) QueryDiff(baseRev, targetRev string) ([]tree.DiffEntry, error) {
	u := fmt.Sprintf("%s/api/v1/query/diff?base=%s&target=%s", c.baseURL, url.QueryEscape(baseRev), url.QueryEscape(targetRev))
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query diff failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var diffs []tree.DiffEntry
	if err := json.NewDecoder(resp.Body).Decode(&diffs); err != nil {
		return nil, err
	}
	return diffs, nil
}

func (c *Client) GetQueueStatus(repoName string) (map[string]any, error) {
	u := fmt.Sprintf("%s/api/v1/queue/status?repo=%s", c.baseURL, url.QueryEscape(repoName))
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("queue status failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return status, nil
}
