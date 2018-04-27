package main

import (
	"encoding/json"
	"errors"
	"fmt"
	cm "github.com/chartmuseum/helm-push/pkg/chartmuseum"
	"github.com/chartmuseum/helm-push/pkg/helm"
	"github.com/spf13/cobra"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"net/url"
	"strconv"
)

type (
	pushCmd struct {
		chartName    string
		chartVersion string
		repoName     string
		username     string
		password     string
		accessToken  string
		contextPath  string
		useHTTP      bool
	}
)

var (
	globalUsage = `Helm plugin to push chart package to ChartMuseum

Examples:

  $ helm push mychart-0.1.0.tgz chartmuseum       # push .tgz from "helm package"
  $ helm push . chartmuseum                       # package and push chart directory
  $ helm push . --version="7c4d121" chartmuseum   # override version in Chart.yaml
`
)

func newPushCmd(args []string) *cobra.Command {
	p := &pushCmd{}
	cmd := &cobra.Command{
		Use:          "helm push",
		Short:        "Helm plugin to push chart package to ChartMuseum",
		Long:         globalUsage,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {

			// If there are 4 args, this is likely being used as a downloader for cm:// protocol
			if len(args) == 4 && strings.HasPrefix(args[3], "cm://") {
				p.setFieldsFromEnv()
				return p.download(args[3])
			}

			if len(args) != 2 {
				return errors.New("This command needs 2 arguments: name of chart, name of chart repository")
			}
			p.chartName = args[0]
			p.repoName = args[1]
			p.setFieldsFromEnv()
			return p.push()
		},
	}
	f := cmd.Flags()
	f.StringVarP(&p.chartVersion, "version", "v", "", "Override chart version pre-push")
	f.StringVarP(&p.username, "username", "u", "", "Override HTTP basic auth username [$HELM_REPO_USERNAME]")
	f.StringVarP(&p.password, "password", "p", "", "Override HTTP basic auth password [$HELM_REPO_PASSWORD]")
	f.StringVarP(&p.accessToken, "access-token", "", "", "Send token in authorization header [$HELM_REPO_ACCESS_TOKEN]")
	f.StringVarP(&p.contextPath, "context-path", "", "", "ChartMuseum context path [$HELM_REPO_CONTEXT_PATH]")
	f.Parse(args)
	return cmd
}

func (p *pushCmd) setFieldsFromEnv() {
	if v, ok := os.LookupEnv("HELM_REPO_USERNAME"); ok && p.username == "" {
		p.username = v
	}
	if v, ok := os.LookupEnv("HELM_REPO_PASSWORD"); ok && p.password == "" {
		p.password = v
	}
	if v, ok := os.LookupEnv("HELM_REPO_ACCESS_TOKEN"); ok && p.accessToken == "" {
		p.accessToken = v
	}
	if v, ok := os.LookupEnv("HELM_REPO_CONTEXT_PATH"); ok && p.contextPath == "" {
		p.contextPath = v
	}
	if v, ok := os.LookupEnv("HELM_REPO_USE_HTTP"); ok {
		p.useHTTP, _ = strconv.ParseBool(v)
	}
}

func (p *pushCmd) push() error {
	repo, err := helm.GetRepoByName(p.repoName)
	if err != nil {
		return err
	}

	chart, err := helm.GetChartByName(p.chartName)
	if err != nil {
		return err
	}

	// version override
	if p.chartVersion != "" {
		chart.SetVersion(p.chartVersion)
	}

	// username/password override(s)
	username := repo.Username
	password := repo.Password
	if p.username != "" {
		username = p.username
	}
	if p.password != "" {
		password = p.password
	}

	client := cm.NewClient(
		cm.URL(repo.URL),
		cm.Username(username),
		cm.Password(password),
		cm.AccessToken(p.accessToken),
		cm.ContextPath(p.contextPath),
	)

	tmp, err := ioutil.TempDir("", "helm-push-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	chartPackagePath, err := helm.CreateChartPackage(chart, tmp)
	if err != nil {
		return err
	}

	fmt.Printf("Pushing %s to %s...\n", filepath.Base(chartPackagePath), p.repoName)
	resp, err := client.UploadChartPackage(chartPackagePath)
	if err != nil {
		return err
	}

	return handlePushResponse(resp)
}

func (p *pushCmd) download(fileURL string) error {
	fmt.Println(fileURL)
	parsedURL, err := url.Parse(fileURL)
	if err != nil {
		return err
	}

	fmt.Println(parsedURL.Path)
	parts := strings.Split(parsedURL.Path, "/")
	numParts := len(parts)
	if numParts <= 1 {
		return fmt.Errorf("invalid file url: %s", fileURL)
	}

	fmt.Println(parts)
	filePath := parts[numParts-1]

	numRemoveParts := 1
	if parts[numParts-2] == "charts" {
		numRemoveParts++
		filePath = "charts/" + filePath
	}

	parsedURL.Path = strings.Join(parts[:numParts - numRemoveParts], "/")

	if p.useHTTP {
		parsedURL.Scheme = "http"
	} else {
		parsedURL.Scheme = "https"
	}

	client := cm.NewClient(
		cm.URL(parsedURL.String()),
		cm.Username(p.username),
		cm.Password(p.password),
		cm.AccessToken(p.accessToken),
		cm.ContextPath(p.contextPath),
	)

	contents, err := client.DownloadFile(filePath)
	if err != nil {
		return err
	}

	fmt.Print(string(contents))
	return nil
}

func handlePushResponse(resp *http.Response) error {
	if resp.StatusCode != 201 {
		b, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		if err != nil {
			return err
		}
		var er struct {
			Error string `json:"error"`
		}
		err = json.Unmarshal(b, &er)
		if err != nil || er.Error == "" {
			return fmt.Errorf("%d: could not properly parse response JSON: %s", resp.StatusCode, string(b))
		}
		return fmt.Errorf("%d: %s", resp.StatusCode, er.Error)
	}
	fmt.Println("Done.")
	return nil
}

func main() {
	cmd := newPushCmd(os.Args[1:])
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}