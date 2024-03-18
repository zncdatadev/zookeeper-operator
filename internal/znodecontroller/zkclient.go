package znodecontroller

import (
	"fmt"
	"github.com/samuel/go-zookeeper/zk"
	"time"
)

type ZkClientRepository interface {
	// Create a znode with the given path and data
	Create(path string, data []byte) error

	// Delete the znode with the given path
	Delete(path string) error

	// Close the connection
	Close()

	// Exists weather the znode with the given path exists
	Exists(path string) (bool, error)
}

type ZkClient struct {
	// The address of the zookeeper server
	Address string

	// The zk client
	Client *zk.Conn
}

// NewZkClient new zk client
func NewZkClient(address string) *ZkClient {
	conn := GetConnect([]string{address})
	return &ZkClient{
		Address: address,
		Client:  conn,
	}
}

func GetConnect(zkList []string) (conn *zk.Conn) {
	conn, _, err := zk.Connect(zkList, 10*time.Second)
	if err != nil {
		fmt.Println(err)
	}
	return
}

func (z ZkClient) Create(path string, data []byte) error {
	//flag == 0 is a persistent node
	//flag == zk.FlagEphemeral is a ephemeral node
	//flag == zk.FlagSequence is a sequence node
	_, err := z.Client.Create(path, data, 0, zk.WorldACL(zk.PermAll))
	if err != nil {
		return err
	}
	return nil
}

func (z ZkClient) Delete(path string) error {
	// version == -1 is delete all version
	err := z.Client.Delete(path, -1)
	if err != nil {
		return err
	}
	return nil
}

func (z ZkClient) Close() {
	z.Client.Close()
}

func (z ZkClient) Exists(path string) (bool, error) {
	exists, _, err := z.Client.Exists(path)
	if err != nil {
		return false, err
	}
	return exists, nil
}
