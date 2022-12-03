package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type ChatMessage struct {
	Username string `json:"username"`
	Text     string `json:"text"`
}

var (
	rdb *redis.Client
)

var clients = make(map[*websocket.Conn]bool)
var broadcaster = make(chan ChatMessage)
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	// ensure connection close when function returns
	defer ws.Close()
	clients[ws] = true

	// if it's zero, no messages were ever sent/saved
	if rdb.Exists("chat_messages").Val() != 0 {
		sendPreviousMessages(ws)
	}

	for {
		var msg ChatMessage
		// Read in a new message as JSON and map it to a Message object
		err := ws.ReadJSON(&msg)
		if err != nil {
			delete(clients, ws)
			break
		}
		// send new message to the channel
		broadcaster <- msg
	}
}

func sendPreviousMessages(ws *websocket.Conn) {
	chatMessages, err := rdb.LRange("chat_messages", 0, -1).Result()
	if err != nil {
		panic(err)
	}

	// send previous messages
	for _, chatMessage := range chatMessages {
		var msg ChatMessage
		json.Unmarshal([]byte(chatMessage), &msg)
		messageClient(ws, msg)
	}
}

// If a message is sent while a client is closing, ignore the error
func unsafeError(err error) bool {
	return !websocket.IsCloseError(err, websocket.CloseGoingAway) && err != io.EOF
}

func handleMessages() {
	for {
		// grab any next message from channel
		msg := <-broadcaster

		storeInRedis(msg)
		messageClients(msg)
	}
}

func storeInRedis(msg ChatMessage) {
	json, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}

	if err := rdb.RPush("chat_messages", json).Err(); err != nil {
		panic(err)
	}
}

func messageClients(msg ChatMessage) {
	// send to every client currently connected
	for client := range clients {
		messageClient(client, msg)
	}
}

func messageClient(client *websocket.Conn, msg ChatMessage) {
	err := client.WriteJSON(msg)
	if err != nil && unsafeError(err) {
		log.Printf("error: %v", err)
		client.Close()
		delete(clients, client)
	}
}

//Oussama

func ConnectToDatabase() (*mongo.Client, context.Context) {
	var databaseUri = os.Getenv("DATABASE_URI")

	client, err := mongo.NewClient(options.Client().ApplyURI(databaseUri))
	if err != nil {
		log.Fatal(err)
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	err = client.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}
	return client, ctx
}

func GetData(client *mongo.Client, ctx context.Context) []bson.M {
	var databaseName = os.Getenv("DATABASE_NAME")
	var databaseCollection = os.Getenv("DATABASE_COLLECTION")

	database := client.Database(databaseName)
	collection := database.Collection(databaseCollection)
	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		log.Fatal(err)
	}
	var data []bson.M
	if err = cursor.All(ctx, &data); err != nil {
		log.Fatal(err)
	}
	fmt.Println(data)
	return data
}

func CreateAPI(data []bson.M) {
	r := gin.Default()
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, data)
	})
	err := r.Run()
	if err != nil {
		log.Fatal(err)
	} // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	//port := os.Getenv("PORT")

	client, ctx := ConnectToDatabase()
	data := GetData(client, ctx)
	CreateAPI(data)
	defer func(client *mongo.Client, ctx context.Context) {
		var err = client.Disconnect(ctx)
		if err != nil {
			log.Fatal(err)
		}
	}(client, ctx)

	//redisURL := os.Getenv("REDIS_URL")
	//opt, err := redis.ParseURL(redisURL)
	if err != nil {
		panic(err)
	}
	//rdb = redis.NewClient(opt)
	/*
		http.HandleFunc("/websocket", handleConnections)

		http.Handle("/", http.FileServer(http.Dir("./public")))
		log.Print("Server starting at localhost:4444")
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal(err)
		}

	*/
}
