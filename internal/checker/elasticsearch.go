package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type ElasticsearchChecker struct{}

func (c *ElasticsearchChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		url := req.URI
		if url == "" {
			host := req.Host
			port := req.Port
			if port == 0 {
				port = 9200
			}
			url = fmt.Sprintf("http://%s:%d", host, port)
		}

		httpReq, err := http.NewRequestWithContext(ctx, "GET", url+"/_cluster/health", nil)
		if err != nil {
			return "", err
		}
		if req.Username != "" {
			httpReq.SetBasicAuth(req.Username, req.Password)
		}

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		var health struct {
			Status      string `json:"status"`
			ClusterName string `json:"cluster_name"`
			NumberOfNodes int  `json:"number_of_nodes"`
		}
		if err := json.Unmarshal(body, &health); err != nil {
			return fmt.Sprintf("HTTP %d", resp.StatusCode), nil
		}
		return fmt.Sprintf("Cluster: %s, Status: %s, Nodes: %d", health.ClusterName, health.Status, health.NumberOfNodes), nil
	})
}
