package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

	yaml "gopkg.in/yaml.v3"
)

type Container struct {
	Name  string `yaml:"name"`
	Usage struct {
		Cpu    string `yaml:"cpu"`
		Memory string `yaml:"memory"`
	} `yaml:"usage"`
}

type ResourceYml struct {
	ApiVersion string      `yaml:"apiVersion"`
	Containers []Container `yaml:"containers"`
	Kind       string      `yaml:"kind"`
	Metadata   struct {
		CreationTimestamp string            `yaml:"creationTimestamp"`
		Labels            map[string]string `yaml:"labels"`
		Name              string            `yaml:"name"`
		Namespace         string            `yaml:"namespace"`
	} `yaml:"metadata"`
	Timestamp string `yaml:"timestamp"`
	Window    string `yaml:"window"`
}

type PodMetric struct {
	PodName     string
	Namespace   string // Every TAP component has separate namespace, we can map CPU and Memory usage with this
	Containers  []Container
	TotalCPU    string
	TotalMemory string
}

// we can sum all podMetric per namespace and that will be total CPU and Memory usage of that TAP component ?

type NamespaceCPUMemUsage struct {
	PodsMetric map[string][]PodMetric // namespace will be key
	Lock       sync.Mutex
}

var Namespacemetric = NamespaceCPUMemUsage{
	PodsMetric: make(map[string][]PodMetric),
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

	fmt.Errorf("Started CPU-Memory reading\n")

	// List all namespaces
	err, out = runCmd([]string{"get", "ns", "--no-headers", "-o", "custom-columns=:metadata.name"})
	if err != nil {
		fmt.Errorf("ns error: ", err.Error())
		return
	}

	ns_list := strings.Split(out, "\n")

	for _, ns := range ns_list {
		wg.Add(1)
		go pod_list(ns, wg)
	}

	wg.Wait()
	printMetrics()
	writeIntoFile()
}

func pod_list(ns string, wg *sync.WaitGroup) {

	defer wg.Done()
	var (
		err error
		out string
	)

	wg_pod := new(sync.WaitGroup)

	// List all pods in namespace
	err, out = runCmd([]string{"get", "pods", "-n", ns, "--no-headers", "-o", "custom-columns=:metadata.name"})
	if err != nil {
		fmt.Errorf("Error-pod: %s", err.Error())
		return
	}

	pod_list := strings.Split(out, "\n")

	for _, pod := range pod_list {
		if pod == "" {
			continue
		}
		wg_pod.Add(1)
		go fetch_cpu_mem_usage(pod, ns, wg_pod)

	}
	wg_pod.Wait()
}

func fetch_cpu_mem_usage(pod, ns string, wg *sync.WaitGroup) {

	defer wg.Done()

	// Fetch podMetrics for pods, it will give metrics per container in given pod
	err, out := runCmd([]string{"get", "PodMetrics", pod, "-n", ns, "-oyaml"})
	if err != nil {
		fmt.Errorf("Error-data-fetch: %s", err.Error())
		return
	}

	resYml := ResourceYml{}

	err = yaml.Unmarshal([]byte(out), &resYml)
	if err != nil {
		fmt.Errorf("Unmarshal error: %s\n", err.Error())
		return
	}

	// Fetch podMetric of pod, it will give metric per pod
	err, out = runCmd([]string{"get", "PodMetrics", pod, "-n", ns, "--no-headers"})
	if err != nil {
		fmt.Errorf("Error-data-fetch: %s", err.Error())
		return
	}
	usageByPod := strings.Split(out, "  ")

	temp := PodMetric{
		PodName:     pod,
		Namespace:   ns,
		Containers:  resYml.Containers,
		TotalCPU:    usageByPod[1],
		TotalMemory: usageByPod[2],
	}

	writeToMap(&temp, ns)
}

func writeToMap(tmp *PodMetric, ns string) {

	Namespacemetric.Lock.Lock()
	defer Namespacemetric.Lock.Unlock()
	Namespacemetric.PodsMetric[ns] = append(Namespacemetric.PodsMetric[ns], *tmp)
}

func printMetrics() {
	Namespacemetric.Lock.Lock()
	defer Namespacemetric.Lock.Unlock()
	fmt.Printf("\nCPU and Memory usage by TAP: %+v\n", Namespacemetric.PodsMetric)
}

func writeIntoFile() {

	file, err := os.Create("cpu_mem_usage.csv")
	if err != nil {
		log.Fatalln("File creation error: %s\n", err.Error())
	}
	defer file.Close()

	w := csv.NewWriter(file)
	defer w.Flush()

	row := []string{"Namespace", "PodName", "ContainerName", "CPU Usage", "Memory Usage"}
	if err := w.Write(row); err != nil {
		log.Fatalln("error writing record to file", err)
		return
	}

	Namespacemetric.Lock.Lock()
	defer Namespacemetric.Lock.Unlock()
	for ns, podMetrics := range Namespacemetric.PodsMetric {
		row = []string{ns}
		if err := w.Write(row); err != nil {
			log.Fatalln("error writing record to file", err)
			return
		}
		for _, podMetric := range podMetrics {
			row = []string{" ", podMetric.PodName, " ", podMetric.TotalCPU, podMetric.TotalMemory}
			if err := w.Write(row); err != nil {
				log.Fatalln("error writing record to file", err)
				return
			}
			for _, container := range podMetric.Containers {
				row = []string{" ", " ", container.Name, container.Usage.Cpu, container.Usage.Memory}
				if err := w.Write(row); err != nil {
					log.Fatalln("error writing record to file", err)
					return
				}
			}
		}

	}
}
