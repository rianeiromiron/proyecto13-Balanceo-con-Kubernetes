package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	saDir        = "/var/run/secrets/kubernetes.io/serviceaccount"
	backendLabel = "app=backend"
	deployName   = "backend"
	minReplicas  = 1
	maxReplicas  = 8
)

var k8sClient *http.Client

// initK8sClient arma el cliente HTTP para hablar con el API server in-cluster,
// confiando en la CA del cluster (montada automáticamente por Kubernetes en
// cada Pod que tiene un ServiceAccount). No usamos client-go a propósito,
// para mantener el mismo estilo "sin dependencias pesadas" del resto del proyecto.
func initK8sClient() error {
	caCert, err := os.ReadFile(saDir + "/ca.crt")
	if err != nil {
		return fmt.Errorf("leyendo ca.crt: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return fmt.Errorf("no se pudo parsear ca.crt")
	}
	k8sClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}
	return nil
}

func k8sBaseURL() string {
	return "https://" + os.Getenv("KUBERNETES_SERVICE_HOST") + ":" + os.Getenv("KUBERNETES_SERVICE_PORT")
}

func k8sNamespace() string {
	ns := os.Getenv("POD_NAMESPACE")
	if ns == "" {
		ns = "default"
	}
	return ns
}

// k8sToken relee el token en cada llamada (no lo cachea): el token
// proyectado por Kubernetes rota en el mismo archivo cada ~1h.
func k8sToken() (string, error) {
	b, err := os.ReadFile(saDir + "/token")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func k8sRequest(method, path string, body []byte) (*http.Response, error) {
	token, err := k8sToken()
	if err != nil {
		return nil, err
	}

	var reader *strings.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	} else {
		reader = strings.NewReader("")
	}

	req, err := http.NewRequest(method, k8sBaseURL()+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if method == http.MethodPatch {
		req.Header.Set("Content-Type", "application/merge-patch+json")
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := k8sClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("%s %s devolvió %d", method, path, resp.StatusCode)
	}
	return resp, nil
}

type podInfo struct {
	Name string
	IP   string
}

// listBackendPods lista los Pods del Deployment "backend" que están
// Running y con condición Ready=True. Se usa tanto para dibujar las cajas
// en la GUI como para repartir el broadcast de /sleep por IP de Pod.
func listBackendPods() ([]podInfo, error) {
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods?labelSelector=%s", k8sNamespace(), backendLabel)
	resp, err := k8sRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase      string `json:"phase"`
				PodIP      string `json:"podIP"`
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}

	var pods []podInfo
	for _, it := range list.Items {
		if it.Status.Phase != "Running" || it.Status.PodIP == "" {
			continue
		}
		ready := false
		for _, c := range it.Status.Conditions {
			if c.Type == "Ready" && c.Status == "True" {
				ready = true
				break
			}
		}
		if ready {
			pods = append(pods, podInfo{Name: it.Metadata.Name, IP: it.Status.PodIP})
		}
	}
	return pods, nil
}

// getReplicas / setReplicas usan el subrecurso /scale del Deployment
// "backend" - lo mismo que hace "kubectl scale" por debajo.
func getReplicas() (int, error) {
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments/%s/scale", k8sNamespace(), deployName)
	resp, err := k8sRequest(http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var scale struct {
		Spec struct {
			Replicas int `json:"replicas"`
		} `json:"spec"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&scale); err != nil {
		return 0, err
	}
	return scale.Spec.Replicas, nil
}

func setReplicas(n int) (int, error) {
	if n < minReplicas {
		n = minReplicas
	}
	if n > maxReplicas {
		n = maxReplicas
	}

	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments/%s/scale", k8sNamespace(), deployName)
	body, _ := json.Marshal(map[string]any{"spec": map[string]any{"replicas": n}})
	resp, err := k8sRequest(http.MethodPatch, path, body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return n, nil
}

// broadcastSleepToPods avisa a cada Pod backend actual (por IP directa,
// ya que los Pods no tienen un nombre DNS estable individual) del nuevo
// tiempo de procesamiento.
func broadcastSleepToPods(ms int) {
	pods, err := listBackendPods()
	if err != nil {
		log.Printf("[generator] error listando pods para broadcast de sleep: %v", err)
		return
	}
	body, _ := json.Marshal(map[string]int{"ms": ms})
	for _, p := range pods {
		go func(p podInfo) {
			url := "http://" + p.IP + ":8081/sleep"
			resp, err := backendHTTPClient.Post(url, "application/json", strings.NewReader(string(body)))
			if err != nil {
				log.Printf("[generator] error avisando sleep a %s (%s): %v", p.Name, p.IP, err)
				return
			}
			resp.Body.Close()
		}(p)
	}
}
