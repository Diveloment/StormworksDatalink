package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

var (
	clientInfo     Client
	vessels        = make(map[string]VesselTelemetry)
	vesselsMu      sync.RWMutex
	cleanupEnabled = true
)

const (
	cleanupInterval  = 2500 * time.Microsecond
	cleanupThreshold = 5 * time.Second
)

type Config struct {
	Port int `json:"port"`
}

type Client struct {
	UUID uuid.UUID `json:"uuid"`
}

type VesselTelemetry struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Callsign  string     `json:"callsign"`
	X         float64    `json:"x"`
	Y         float64    `json:"y"`
	Z         float64    `json:"z"`
	ABSSpeed  float64    `json:"absspeed"`
	Type      VesselType `json:"type"`
	Direction float64    `json:"direction"`
	Timestamp int64      `json:"timestamp"`
	TgtX      float64    `json:"tgtx"`
	TgtY      float64    `json:"tgty"`
	TgtZ      float64    `json:"tgtz"`
	HasTgt    bool       `json:"hastgt"`
}

func getExecDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return ".", err
	}
	return filepath.Dir(exePath), nil
}

func loadConfig(filename string) (*Config, error) {
	execDir, err := getExecDir()
	if err != nil {
		execDir = "."
	}

	configPath := filepath.Join(execDir, filename)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return createDefaultConfig(configPath)
	}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func createDefaultConfig(path string) (*Config, error) {
	config := Config{
		Port: 8000,
	}

	file, err := os.Create(path)
	if err != nil {
		return &config, err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(&config); err != nil {
		return &config, err
	}

	fmt.Printf("Создан файл конфигурации: %s\n", path)
	return &config, nil
}

func cleanupOldVessels() {
	for {
		time.Sleep(cleanupInterval)

		if !cleanupEnabled {
			continue
		}

		vesselsMu.Lock()
		now := time.Now().UnixMilli()
		threshold := now - int64(cleanupThreshold/time.Millisecond)

		removedCount := 0
		for id, vessel := range vessels {
			if vessel.Timestamp < threshold {
				delete(vessels, id)
				removedCount++
			}
		}

		if removedCount > 0 {
			fmt.Printf("Удалено %d старых записей (старше %v)\n", removedCount, cleanupThreshold)
			fmt.Printf("Осталось записей: %d\n", len(vessels))
		}

		vesselsMu.Unlock()
	}
}

func main() {
	config, err := loadConfig("config.json")
	if err != nil {
		fmt.Printf("Ошибка загрузки конфигурации: %v\n", err)
		return
	}

	clientInfo.UUID = uuid.New()

	fmt.Printf("Порт: %d\n", config.Port)
	fmt.Println("Запуск сервера...")

	addr := net.JoinHostPort("localhost", fmt.Sprintf("%d", config.Port))

	http.HandleFunc("/info", infoGetHandler)
	http.HandleFunc("/telemetry/setVesselTelemetry", setVesselTelemetry)
	http.HandleFunc("/telemetry/getVessels", getTelemetryData)

	go cleanupOldVessels()

	go func() {
		err := http.ListenAndServe(addr, nil)
		if err != nil {
			fmt.Printf("Ошибка запуска сервера: %v\n", err)
			os.Exit(1)
		}
	}()

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		fmt.Printf("Сервер не запустился: %v\n", err)
		os.Exit(1)
	}
	conn.Close()

	fmt.Printf("Сервер успешно запущен на порту %d\n", config.Port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	fmt.Println("\n Сервер остановлен")
}

func infoGetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Printf("Recieve health check request from %s\n", r.Host)
	data := map[string]interface{}{
		"UUID": clientInfo.UUID,
	}
	json.NewEncoder(w).Encode(data)
}

func setVesselTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	params := r.URL.Query()

	vehId := params.Get("veh_id")
	vehXPos := params.Get("veh_x")
	vehYPos := params.Get("veh_y")
	vehZPos := params.Get("veh_z")
	vehAbsSpeed := params.Get("veh_abs_spd")
	vehDir := params.Get("veh_dir")
	veh_name := params.Get("veh_name")
	callsign := params.Get("callsign")
	vesselType := params.Get("type")
	tgtXPos := params.Get("tgt_x")
	tgtYPos := params.Get("tgt_y")
	tgtZPos := params.Get("tgt_z")

	if vehId == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errorcode": ErrorWrongID.ToInt32(),
		})
		return
	}

	if vesselType == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errorcode": ErrorWrongVesselType.ToInt32(),
		})
		return
	}

	vehIdInt, err := strconv.ParseInt(vehId, 0, 64)
	if err != nil {
		fmt.Printf("Not valid veh_id: %s; set to -1\n", vehId)
		vehIdInt = -1
	}

	tgtXf, tgtYf := parseFloat(tgtXPos), parseFloat(tgtYPos)
	hasTgt := tgtXf != 0 && tgtYf != 0

	telemetry := VesselTelemetry{
		ID:        vehIdInt,
		Name:      veh_name,
		Callsign:  callsign,
		X:         parseFloat(vehXPos),
		Y:         parseFloat(vehYPos),
		Z:         parseFloat(vehZPos),
		ABSSpeed:  parseFloat(vehAbsSpeed),
		Type:      VesselTypeFromInt32(parseInt32(vesselType)),
		Direction: parseFloat(vehDir),
		Timestamp: time.Now().UnixMilli(),
		TgtX:      tgtXf,
		TgtY:      tgtYf,
		TgtZ:      parseFloat(tgtZPos),
		HasTgt:    hasTgt,
	}

	vesselsMu.Lock()
	vessels[vehId] = telemetry
	vesselsMu.Unlock()

	fmt.Printf("Saved telemetry for vessel %s: X=%f, Y=%f, Z=%f, Direction=%f\n",
		vehId, telemetry.X, telemetry.Y, telemetry.Z, telemetry.Direction)

	vesselsMu.RLock()
	vesselsList := make([]VesselTelemetry, 0, len(vessels))
	for _, v := range vessels {
		vesselsList = append(vesselsList, v)
	}
	vesselsMu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"errorcode": ErrorSuccess.ToInt32(),
		"vessels":   vesselsList,
		"count":     len(vesselsList),
		"timestamp": time.Now().UnixMilli(),
	})
}

func getTelemetryData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vesselsMu.RLock()
	vesselsList := make([]VesselTelemetry, 0, len(vessels))
	for _, v := range vessels {
		vesselsList = append(vesselsList, v)
	}
	vesselsMu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"errorcode": ErrorSuccess.ToInt32(),
		"vessels":   vesselsList,
		"count":     len(vesselsList),
		"timestamp": time.Now().UnixMilli(),
	})
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func parseInt32(s string) int32 {
	var i int32
	fmt.Sscanf(s, "%d", &i)
	return i
}
