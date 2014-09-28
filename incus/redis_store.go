package incus

import (
	"errors"
	"log"

	"github.com/gosexy/redis"
)

const ClientsKey = "SocketClients"
const PageKey = "PageClients"

type RedisStore struct {
	clientsKey string
	pageKey    string

	server string
	port   uint
	pool   redisPool
}

//connection pool implimentation
type redisPool struct {
	connections chan *redis.Client
	maxIdle     int
	connFn      func() (*redis.Client, error) // function to create new connection.
}

func newRedisStore(redis_host string, redis_port uint) *RedisStore {

	return &RedisStore{
		ClientsKey,
		PageKey,

		redis_host,
		redis_port,

		redisPool{
			connections: make(chan *redis.Client, 6),
			maxIdle:     6,

			connFn: func() (*redis.Client, error) {
				client := redis.New()
				err := client.Connect(redis_host, redis_port)

				if err != nil {
					log.Printf("Redis connect failed: %s\n", err.Error())
					return nil, err
				}

				return client, nil
			},
		},
	}

}

func (this *redisPool) Get() (*redis.Client, bool) {

	var conn *redis.Client
	select {
	case conn = <-this.connections:
	default:
		conn, err := this.connFn()
		if err != nil {
			return nil, false
		}

		return conn, true
	}

	if err := this.testConn(conn); err != nil {
		return this.Get() // if connection is bad, get the next one in line until base case is hit, then create new client
	}

	return conn, true
}

func (this *redisPool) Close(conn *redis.Client) {
	select {
	case this.connections <- conn:
		return
	default:
		conn.Quit()
	}
}

func (this *redisPool) testConn(conn *redis.Client) error {
	if _, err := conn.Ping(); err != nil {
		conn.Quit()
		return err
	}

	return nil
}

func (this *RedisStore) GetConn() (*redis.Client, error) {

	client, ok := this.pool.Get()
	if !ok {
		return nil, errors.New("Error while getting redis connection")
	}

	return client, nil
}

func (this *RedisStore) CloseConn(conn *redis.Client) {
	this.pool.Close(conn)
}

func (this *RedisStore) Subscribe(c chan []string, channel string) (*redis.Client, error) {
	consumer := redis.New()
	err := consumer.ConnectNonBlock(this.server, this.port)
	if err != nil {
		return nil, err
	}

	if _, err := consumer.Ping(); err != nil {
		return nil, err
	}

	go consumer.Subscribe(c, channel)
	<-c // ignore subscribe command

	return consumer, nil
}

func (this *RedisStore) Publish(channel string, message string) {
	publisher, err := this.GetConn()
	if err != nil {
		return
	}
	defer this.CloseConn(publisher)

	publisher.Publish(channel, message)
}

func (this *RedisStore) Save(sock *Socket) error {
	client, err := this.GetConn()
	if err != nil {
		return err
	}
	defer this.CloseConn(client)

	_, err = client.SAdd(this.clientsKey, sock.UID)
	if err != nil {
		return err
	}

	return nil
}

func (this *RedisStore) Remove(sock *Socket) error {
	client, err := this.GetConn()
	if err != nil {
		return err
	}
	defer this.CloseConn(client)

	_, err = client.SRem(this.clientsKey, sock.UID)
	if err != nil {
		return err
	}

	return nil
}

func (this *RedisStore) Clients() ([]string, error) {
	client, err := this.GetConn()
	if err != nil {
		return nil, err
	}
	defer this.CloseConn(client)

	socks, err1 := client.SMembers(this.clientsKey)
	if err1 != nil {
		return nil, err1
	}

	return socks, nil
}

func (this *RedisStore) Count() (int64, error) {
	client, err := this.GetConn()
	if err != nil {
		return 0, err
	}
	defer this.CloseConn(client)

	socks, err1 := client.SCard(this.clientsKey)
	if err1 != nil {
		return 0, err1
	}

	return socks, nil
}

func (this *RedisStore) SetPage(sock *Socket) error {
	client, err := this.GetConn()
	if err != nil {
		return err
	}
	defer this.CloseConn(client)

	_, err = client.HIncrBy(this.pageKey, sock.Page, 1)
	if err != nil {
		return err
	}

	return nil
}

func (this *RedisStore) UnsetPage(sock *Socket) error {
	client, err := this.GetConn()
	if err != nil {
		return err
	}
	defer this.CloseConn(client)

	var i int64
	i, err = client.HIncrBy(this.pageKey, sock.Page, -1)
	if err != nil {
		return err
	}

	if i <= 0 {
		client.HDel(this.pageKey, sock.Page)
	}

	return nil
}
