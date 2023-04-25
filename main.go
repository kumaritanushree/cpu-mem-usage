package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	yaml "gopkg.in/yaml.v3"
)

type ResourceYml struct {
	ApiVersion string `yaml:"apiVersion"`
	Containers []struct {
		Name  string `yaml:"name"`
		Usage struct {
			Cpu    string `yaml:"cpu"`
			Memory string `yaml:"memory"`
		} `yaml:"usage"`
	} `yaml:"containers"`
	Kind     string `yaml:"kind"`
	Metadata struct {
		CreationTimestamp string            `yaml:"creationTimestamp"`
		Labels            map[string]string `yaml:"labels"`
		Name              string            `yaml:"name"`
		Namespace         string            `yaml:"namespace"`
	} `yaml:"metadata"`
	Timestamp string `yaml:"timestamp"`
	Window    string `yaml:"window"`
}

type FileInfo struct {
	fileLock sync.Mutex
	filePtr  *os.File
}

func runCmd(args []string) (error, string) {

	var (
		stderr, stdout bytes.Buffer
	)

	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Error in run cmd: %s\n", stderr.String()), ""
	}

	return nil, stdout.String()
}

func main() {

	var (
		err error
		out string
	)

	wg := new(sync.WaitGroup)

	fmt.Printf("Started CPU-Memory reading\n")

	// List all namespaces
	err, out = runCmd([]string{"get", "ns", "--no-headers", "-o", "custom-columns=:metadata.name"})
	if err != nil {
		fmt.Print("ns error: ", err.Error())
		return
	}

	ns_list := strings.Split(out, "\n")

	file, err := os.OpenFile("cpu_mem_usage2.txt", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		fmt.Printf("File creation error: %s\n", err.Error())
	}

	f := FileInfo{filePtr: file}

	defer file.Close()

	for _, ns := range ns_list {
		wg.Add(1)
		go f.pod_list(ns, wg)
	}

	wg.Wait()
}

func (f *FileInfo) pod_list(ns string, wg *sync.WaitGroup) {

	defer wg.Done()
	var (
		err error
		out string
	)

	wg_pod := new(sync.WaitGroup)

	// List all pods in namespace
	err, out = runCmd([]string{"get", "pods", "-n", ns, "--no-headers", "-o", "custom-columns=:metadata.name"})
	if err != nil {
		fmt.Printf("Error-pod: %s", err.Error())
		return
	}

	pod_list := strings.Split(out, "\n")

	for _, pod := range pod_list {
		if pod == "" {
			continue
		}
		wg_pod.Add(1)
		go f.fetch_cpu_mem_usage(pod, ns, wg_pod)

	}
	wg_pod.Wait()
}

func (f *FileInfo) fetch_cpu_mem_usage(pod, ns string, wg *sync.WaitGroup) {

	defer wg.Done()

	// Fetch podMetrics for pods, it will give metrics per container in given pod
	err, out := runCmd([]string{"get", "PodMetrics", pod, "-n", ns, "-oyaml"})
	if err != nil {
		fmt.Printf("Error-data-fetch: %s", err.Error())
		return
	}

	resYml := ResourceYml{}

	err = yaml.Unmarshal([]byte(out), &resYml)
	if err != nil {
		fmt.Printf("Unmarshal error: %s\n", err.Error())
		return
	}

	// Fetch podMetric of pod, it will give metric of resource used by pod
	err, out = runCmd([]string{"get", "PodMetrics", "build-pod-image-fetcher-4wh4l", "-n", "build-service", "--no-headers"})
	if err != nil {
		fmt.Printf("Error-data-fetch: %s", err.Error())
		return
	}
	usageByPod := strings.Split(out, "  ")

	resUsage := ""
	for _, con := range resYml.Containers {
		resUsage = resUsage + "Container_name: " + con.Name + "\nCPU: " + con.Usage.Cpu + " Memory: " + con.Usage.Memory + "\n"
	}
	resUsage = resUsage + "Usage_by_pod:: CPU: " + usageByPod[1] + " Memory: " + usageByPod[2] + "\n"

	f.fileLock.Lock()
	_, err = f.filePtr.WriteString("Namespace: " + ns + ", Pod: " + pod + "\n" + resUsage + "\n")
	if err != nil {
		fmt.Printf("File writing error: %s\n", err.Error())
	}
	f.fileLock.Unlock()
}

// need to check pod with status=completed,
// need check just after installation also we get status as completed. If yes need to handle that case here.
