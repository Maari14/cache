package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	DBHost     = "localhost"
	DBPort     = "5432"
	DBUser     = "root"
	DBPassword = "root"
	DBName     = "pqtest"
	ServerPort = ":8080"
)

// CacheItem represents a cache entry
type CacheItem struct {
	gorm.Model
	Key     string    `json:"key" gorm:"unique"`
	Value   string    `json:"value"`
	Expires time.Time `json:"expires"`
}

// DB instance
var db *gorm.DB

// In-memory cache
var cache = make(map[string]CacheItem)
var cacheLock = sync.RWMutex{}

// WebSocket clients
var clients = make(map[*websocket.Conn]bool)
var broadcast = make(chan map[string]CacheItem)
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func initDB() {
	var err error
	dsn := "host=" + DBHost + " port=" + DBPort + " user=" + DBUser + " password=" + DBPassword + " dbname=" + DBName + " sslmode=disable"
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	err = db.AutoMigrate(&CacheItem{})
	if err != nil {
		log.Fatal("Failed to perform migrations:", err)
	}

	log.Println("Successfully connected to the database and migrated")
}

func main() {
	initDB()

	// Initialize cache from DB
	initializeCache()

	r := mux.NewRouter()
	r.HandleFunc("/cache/{key}", getCacheItem).Methods("GET")
	r.HandleFunc("/cache", setCacheItem).Methods("POST")
	r.HandleFunc("/ws", wsHandler)

	go handleMessages()

	log.Printf("Listening on port %s", ServerPort)
	log.Fatal(http.ListenAndServe(ServerPort, r))
}

func initializeCache() {
	var items []CacheItem
	if err := db.Find(&items).Error; err != nil {
		log.Println("Error loading cache from DB:", err)
		return
	}
	cacheLock.Lock()
	defer cacheLock.Unlock()
	for _, item := range items {
		if time.Now().Before(item.Expires) {
			cache[item.Key] = item
		} else {
			db.Delete(&item)
		}
	}
}

func getCacheItem(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	key := params["key"]

	cacheLock.RLock()
	defer cacheLock.RUnlock()

	if item, found := cache[key]; found {
		if time.Now().Before(item.Expires) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(item)
			return
		}
		delete(cache, key)
		db.Delete(&item)
	}

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode("Key not found")
}

func setCacheItem(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Key      string        `json:"key"`
		Value    string        `json:"value"`
		Duration time.Duration `json:"duration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	expiration := time.Now().Add(input.Duration * time.Second)
	cacheItem := CacheItem{
		Key:     input.Key,
		Value:   input.Value,
		Expires: expiration,
	}

	cacheLock.Lock()
	cache[input.Key] = cacheItem
	cacheLock.Unlock()

	db.Save(&cacheItem)

	broadcast <- cache

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cacheItem)

}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()

	clients[ws] = true

	for {
		var msg map[string]CacheItem
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("Error: %v", err)
			delete(clients, ws)
			break
		}
	}
}

func handleMessages() {
	for {
		msg := <-broadcast
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("Error: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
}
