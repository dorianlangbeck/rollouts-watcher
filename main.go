package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"

	argov1alpha1 "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
)

var MergeFreezeAccessToken string

func setRespositoryFreeze(ctx context.Context, repo, note string, frozen bool) error {
	body := fmt.Sprintf("frozen=%v&user_name=Argo Rollout Bot&note=%s", frozen, note)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://www.mergefreeze.com/api/branches/%s/main/?access_token=%s", repo, MergeFreezeAccessToken), strings.NewReader(body))
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	return nil
}

func setRepositoryRolloutPhase(ctx context.Context, repo string, phase v1alpha1.RolloutPhase) error {
	return setRespositoryFreeze(ctx, repo, fmt.Sprintf("Rollout phase is %s", phase), phase != v1alpha1.RolloutPhaseHealthy)
}

type Watcher struct {
	argo argov1alpha1.ArgoprojV1alpha1Interface
}

func NewWatcher(argo argov1alpha1.ArgoprojV1alpha1Interface) *Watcher {
	return &Watcher{
		argo: argo,
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	return w.watchRollouts(ctx)
}

func (w *Watcher) watchRollouts(ctx context.Context) error {
	wi, err := w.argo.Rollouts("").Watch(ctx, v1.ListOptions{})
	if err != nil {
		return err
	}

	defer wi.Stop()

	for {
		select {
		case e := <-wi.ResultChan():
			switch e.Type {
			case watch.Error:
				return errors.FromObject(e.Object)

			case watch.Added:
				rollout := e.Object.(*v1alpha1.Rollout)
				repository := rollout.Annotations["repository"]
				if repository == "" {
					continue
				}

				log.Println("ADDED:", rollout.Name, rollout.Status.Phase)
				err := setRepositoryRolloutPhase(ctx, repository, rollout.Status.Phase)
				if err != nil {
					return fmt.Errorf("set repository rollout phase failed: %w", err)
				}

			case watch.Modified:
				rollout := e.Object.(*v1alpha1.Rollout)
				repository := rollout.Annotations["repository"]
				if repository == "" {
					continue
				}

				log.Println("MODIFIED:", rollout.Name, rollout.Status.Phase)
				err := setRepositoryRolloutPhase(ctx, repository, rollout.Status.Phase)
				if err != nil {
					return fmt.Errorf("set repository rollout phase failed: %w", err)
				}
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

var kubeconfig string

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to Kubernetes config file")
	flag.Parse()
}

func run(ctx context.Context) error {
	MergeFreezeAccessToken = os.Getenv("MERGE_FREEZE_ACCESS_TOKEN")
	if MergeFreezeAccessToken == "" {
		return fmt.Errorf("undefined environment variable: MERGE_FREEZE_ACCESS_TOKEN")
	}

	if kubeconfig == "" {
		home := homedir.HomeDir()
		if home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
			_, err := os.Stat(kubeconfig)
			if err != nil {
				kubeconfig = ""
			}
		}
	}

	var config *rest.Config
	var err error
	if kubeconfig == "" {
		config, err = rest.InClusterConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		return fmt.Errorf("could not build Kubernetes configuration: %w", err)
	}

	argocs, err := versioned.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("could not create a argo-rollout clientset: %w", err)
	}

	watcher := NewWatcher(argocs.ArgoprojV1alpha1())
	return watcher.Run(ctx)
}

func main() {
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	err := run(ctx)
	if err != nil {
		log.Fatalf("[FAIL] %s", err.Error())
		return
	}
}
