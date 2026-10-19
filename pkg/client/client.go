package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/queue"
	"github.com/aether/scn/pkg/tree"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(serverURL string) *Client {
	serverURL = strings.TrimSuffix(serverURL, "/")
	return &Client{
		baseURL: serverURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
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

	wireList := make([]chunkWire, 0, len(chunks))
	for _, chunk := range chunks {
		wireList = append(wireList, chunkWire{
			Hash:           chunk.Hash,
			UncompressedSz: chunk.UncompressedSz,
			DataBase64:     base64.StdEncoding.EncodeToString(chunk.Data),
		})
	}

	body, err := json.Marshal(wireList)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/cas/blobs/batch-put", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload chunks failed (%d): %s", resp.StatusCode, string(errResp))
	}
	return nil
}

func (c *Client) DownloadChunks(hashes []string) ([]*cas.BlobChunk, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	reqBody, _ := json.Marshal(map[string][]string{"hashes": hashes})
	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/cas/blobs/batch-get", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errResp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download chunks failed (%d): %s", resp.StatusCode, string(errResp))
	}

	var wireList []chunkWire
	if err := json.NewDecoder(resp.Body).Decode(&wireList); err != nil {
		return nil, err
	}

	chunks := make([]*cas.BlobChunk, 0, len(wireList))
	for _, w := range wireList {
		raw, err := base64.StdEncoding.DecodeString(w.DataBase64)
		if err != nil {
			continue
		}
		chunks = append(chunks, &cas.BlobChunk{
			Hash:           w.Hash,
			UncompressedSz: w.UncompressedSz,
			Data:           raw,
		})
	}
	return chunks, nil
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

