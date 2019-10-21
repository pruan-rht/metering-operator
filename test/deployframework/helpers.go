package deployframework

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	metering "github.com/operator-framework/operator-metering/pkg/apis/metering/v1"
)

func checkPodStatus(pod v1.Pod) (bool, int) {
	if pod.Status.Phase != v1.PodRunning {
		return false, 0
	}

	var unreadyContainers int

	for _, status := range pod.Status.ContainerStatuses {
		if !status.Ready {
			unreadyContainers++
		}
	}

	return unreadyContainers == 0, len(pod.Status.ContainerStatuses) - unreadyContainers
}

func createResourceDirs(namespace, path string) ([]string, error) {
	envVarArr := []string{
		"METERING_TEST_NAMESPACE=" + namespace,
	}

	testDirsMap := map[string]string{
		logDir:              "LOG_DIR",
		reportsDir:          "REPORTS_DIR",
		meteringconfigDir:   "METERINGCONFIGS_DIR",
		datasourcesDir:      "DATASOURCES_DIR",
		reportqueriesDir:    "REPORTQUERIES_DIR",
		hivetablesDir:       "HIVETABLES_DIR",
		prestotablesDir:     "PRESTOTABLES_DIR",
		storagelocationsDir: "STORAGELOCATIONS_DIR",
	}

	for dirname, env := range testDirsMap {
		dirPath := filepath.Join(path, dirname)

		err := os.MkdirAll(dirPath, 0777)
		if err != nil {
			return nil, fmt.Errorf("failed to create the directory %s: %v", dirPath, err)
		}

		envVarArr = append(envVarArr, env+"="+dirPath)
	}

	return envVarArr, nil
}

func logPollingSummary(logger logrus.FieldLogger, targetPods int, readyPods []string, unreadyPods []podStat) {
	logger.Infof("Poll Summary")
	logger.Infof("Current ratio of ready to target pods: %d/%d", len(readyPods), targetPods)

	for _, unreadyPod := range unreadyPods {
		if unreadyPod.Total == 0 {
			logger.Infof("Pod %s is pending", unreadyPod.PodName)
			continue
		}
		logger.Infof("Pod %s has %d/%d ready containers", unreadyPod.PodName, unreadyPod.Ready, unreadyPod.Total)
	}
}

func validateImageConfig(image metering.ImageConfig) error {
	var errArr []string

	if image.Repository == "" {
		errArr = append(errArr, "the image repository is empty")
	}
	if image.Tag == "" {
		errArr = append(errArr, "the image tag is empty")
	}

	if len(errArr) != 0 {
		return fmt.Errorf(strings.Join(errArr, "\n"))
	}

	return nil
}

type PodWaiter struct {
	Logger logrus.FieldLogger
	Client kubernetes.Interface
}

type podStat struct {
	PodName string
	Ready   int
	Total   int
}

// waitForPods periodically polls the list of pods in the namespace
// and ensures the metering pods created are considered ready. In order to exit
// the polling loop, the number of pods listed must match the expected number
// of targetPodsCount, and all pod containers listed must report a ready status.
func (pw *PodWaiter) WaitForPods(namespace string, targetPodsCount int) error {

	err := wait.Poll(10*time.Second, 20*time.Minute, func() (done bool, err error) {
		var readyPods []string
		var unreadyPods []podStat

		pods, err := pw.Client.CoreV1().Pods(namespace).List(meta.ListOptions{})
		if err != nil {
			return false, err
		}

		// TODO(chancez): is this check needed? If so, maybe move outside of
		// WaitForPods.
		if len(pods.Items) == 0 {
			return false, fmt.Errorf("the number of pods in the %s namespace should not be zero", namespace)
		}

		for _, pod := range pods.Items {
			podIsReady, readyContainers := checkPodStatus(pod)
			if podIsReady {
				readyPods = append(readyPods, pod.Name)
				continue
			}

			unreadyPods = append(unreadyPods, podStat{
				PodName: pod.Name,
				Ready:   readyContainers,
				Total:   len(pod.Status.ContainerStatuses),
			})
		}

		if pw.Logger != nil {
			logPollingSummary(pw.Logger, targetPodsCount, readyPods, unreadyPods)
		}

		return len(pods.Items) == targetPodsCount && len(unreadyPods) == 0, nil
	})
	if err != nil {
		return fmt.Errorf("the pods failed to report a ready status before the timeout period occurred: %v", err)
	}

	return nil
}

// GetServiceAccountToken queries the namespace for the service account and attempts
// to find the secret that contains the serviceAccount token and return it.
func GetServiceAccountToken(client kubernetes.Interface, namespace, serviceAccountName string) (string, error) {
	var sa *v1.ServiceAccount
	var err error

	err = wait.Poll(5*time.Second, 5*time.Minute, func() (done bool, err error) {
		sa, err = client.CoreV1().ServiceAccounts(namespace).Get(serviceAccountName, meta.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("error getting service account %s: %v", reportingOperatorServiceAccountName, err)
	}

	if len(sa.Secrets) == 0 {
		return "", fmt.Errorf("service account %s has no secrets", serviceAccountName)
	}

	var secretName string

	for _, secret := range sa.Secrets {
		if strings.Contains(secret.Name, "token") {
			secretName = secret.Name
			break
		}
	}

	if secretName == "" {
		return "", fmt.Errorf("%s service account has no token", serviceAccountName)
	}

	secret, err := client.CoreV1().Secrets(namespace).Get(secretName, meta.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed getting %s service account token secret: %v", serviceAccountName, err)
	}

	return string(secret.Data["token"]), nil
}