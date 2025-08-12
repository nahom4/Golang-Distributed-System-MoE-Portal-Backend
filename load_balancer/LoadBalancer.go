package main

import (
	"context"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/cors"
	clientv3 "go.etcd.io/etcd/client/v3"
)


type DistributedLock struct {
	Key        string
	Value      string
	LeaseID    clientv3.LeaseID
	etcdClient *clientv3.Client
}

type Server struct{
	Address *url.URL
	Latitude float64
	Longitude float64
	
}

type LoadBalancer struct {
	servers []Server
	petitionServers []Server
	mutex   sync.Mutex
	documentWebSockets map[string]*url.URL
}

func (dl *DistributedLock) Lock(ctx context.Context, ttl int64) error {


	lease, err := dl.etcdClient.Grant(ctx, ttl)

	if err != nil {
		return err
	}

	resp, err := dl.etcdClient.Txn(ctx).
	If(clientv3.Compare(clientv3.Version(dl.Key), "=", 0)).
		Then(clientv3.OpPut(dl.Key, dl.Value, clientv3.WithLease(lease.ID))).
		Commit()

	if err != nil {
		return err
	}

	if !resp.Succeeded{
		return errors.New("error acquring lock")
	}

	dl.LeaseID = lease.ID
	log.Printf("Lock acquired: %s", dl.Key)
	return nil
}


func (dl *DistributedLock) Unlock(ctx context.Context) error {
	_, err := dl.etcdClient.Delete(ctx, dl.Key)
	if err != nil {
		return err
	}

	_, err = dl.etcdClient.Revoke(ctx, dl.LeaseID)
	if err != nil {
		return err
	}

	log.Printf("Lock released: %s", dl.Key)
	return nil
}



type Location struct{
	Latitude float64
	Longitude float64
}

type Tuple struct{
	distance float64
	server Server
}
func degToRad(deg float64) float64 {
	return deg * (math.Pi / 180)
}

func Harvsine(serverLocation Location,requestLocation Location)float64{
	R := 6371.0
	lat1 := degToRad(serverLocation.Latitude)
	lat2 := degToRad(requestLocation.Latitude)
	lon1 := degToRad(serverLocation.Longitude)
	lon2 := degToRad(requestLocation.Longitude)
	dlat := lat2 - lat1
	dlon := lon2 - lon1

	// Haversine formula
	a := math.Pow(math.Sin(dlat/2), 2) + math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin(dlon/2), 2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	// Distance
	distance := R * c

	return distance

}

func (lb *LoadBalancer) DistanceCalculator(requestLocation Location,serverList []Server)[]Tuple{
	
	distanceServerMap := []Tuple{}
	
	for _,server := range serverList{
		serverLocation := Location{server.Latitude,server.Longitude}
		distance := Harvsine(serverLocation,requestLocation)
		distanceServerMap = append(distanceServerMap, Tuple{server: server,distance: distance})
	}

	sort.Slice(distanceServerMap,func(i, j int) bool {
		return distanceServerMap[i].distance < distanceServerMap[j].distance
	})

	return distanceServerMap
}

func (lb *LoadBalancer) nextServer(requestLocation Location,serverList []Server) *url.URL {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()
	serverDistanceMap := lb.DistanceCalculator(requestLocation,serverList)

	for _,val := range serverDistanceMap{
		address := val.server.Address
		running,_ := lb.checkHealth(address.String())
		if running{
			return address
		}
	}
	// To be implemented Here if all severs fail
	return &url.URL{}
}

func (lb *LoadBalancer) reverseProxy(server *url.URL,w http.ResponseWriter,r *http.Request){
	proxy := httputil.NewSingleHostReverseProxy(server)
	proxy.ServeHTTP(w, r)
}

func (lb *LoadBalancer) handlePetitionRequest(documentName string,requestLocation Location,w http.ResponseWriter,r *http.Request) {
	
	if server, ok := lb.documentWebSockets[documentName]; ok {
			// check the health of the server
			running,_ := lb.checkHealth(server.String())
			if running{	
				lb.reverseProxy(server,w,r)
				return 
			}
	}

	server := lb.nextServer(requestLocation,lb.petitionServers)
	lb.documentWebSockets[documentName] = server
	lb.reverseProxy(server,w,r)

}

   
func (lb *LoadBalancer) handleRequest(w http.ResponseWriter, r *http.Request) {
	
	lat,_ := strconv.ParseFloat(r.URL.Query().Get("Latitude"), 64)
	long,_ := strconv.ParseFloat(r.URL.Query().Get("Longitude"), 64)
	requestLocation := Location {
		Latitude : lat,
		Longitude: long,
	}


	documentName := r.URL.Query().Get("document")
	tag := r.URL.Query().Get("tag")

	if (documentName != "" || tag == "petition"){
		lb.handlePetitionRequest(documentName,requestLocation,w,r)
		return
	}

	server := lb.nextServer(requestLocation,lb.servers)
	lb.reverseProxy(server,w,r)
}

func (lb *LoadBalancer) start(dl DistributedLock,ctx context.Context){
	print("stat function called")
	for {
		// Acquire the lock
		// err := dl.Lock(ctx, 20) // Set TTL to 10 seconds
		// if err != nil {
		// 	continue
		// }

		activeServerLocation := dl.Value
		isActive, _ := lb.checkHealth(activeServerLocation)
		
		if !isActive{
			// If active server returns False, start listening at the active port
			lb.startListening(activeServerLocation)
			break
		} 

		time.Sleep(time.Second * 10)
		errs := dl.Unlock(ctx) 
		if errs != nil {
		}
	}
}
func (lb *LoadBalancer) checkHealth(address string)(bool,error){
	urlWithoutHTTP := strings.TrimPrefix(address, "http://")
	conn, err := net.DialTimeout("tcp", urlWithoutHTTP, 1*time.Second)
	if err != nil {
		return false,err
	}

	conn.Close()
	return true,err
}
func (lb *LoadBalancer) startListening(address string){
	http.ListenAndServe(address, nil)
}

func main() {

	print("Starting Load Balancer...\n")
	lb := &LoadBalancer{
		servers: []Server{
			Server{
				Address:   parseURL("http://localhost:3030"),
				Latitude:  10.5,
				Longitude: 20.6,
			},
			Server{
				Address:   parseURL("http://localhost:3031"),
				Latitude:  70.5,
				Longitude: 46.5,
			},
		},
		petitionServers : []Server{
			Server{
				Address:   parseURL("http://localhost:3032"),
				Latitude:  10.5,
				Longitude: 20.6,
			},
			Server{
				Address:   parseURL("http://localhost:3033"),
				Latitude:  70.5,
				Longitude: 46.5,
			},
		},
		documentWebSockets: make(map[string]*url.URL),
	
	}

	corsMiddleware := cors.Default()
	handler := corsMiddleware.Handler(http.HandlerFunc(lb.handleRequest))

	

	// http.HandleFunc("/", lb.handleRequest)
	http.Handle("/", handler)

	endpoints := []string{"localhost:2379"}

	cfg := clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	}

	client, err := clientv3.New(cfg)
	if err != nil {
		os.Exit(1)
	}

	defer client.Close()
	port := os.Getenv("PORT")
	ctx := context.Background()
	lockKey := "active-sever-address"
	lockValue := port

	dl := DistributedLock{
		Key:        lockKey,
		Value:      lockValue,
		etcdClient: client,
	}	
	
	print("Load Balancer started at :",port)
	lb.start(dl,ctx)

}

func parseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u
}
