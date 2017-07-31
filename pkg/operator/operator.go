package operator

import (
	"database/sql"
	"fmt"
	"time"

	ext_client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/coreos-inc/kube-chargeback/pkg/chargeback"
	"github.com/coreos-inc/kube-chargeback/pkg/hive"
)

type Config struct {
	HiveHost   string
	PrestoHost string
}

func New(cfg Config) (*Chargeback, error) {
	cb := &Chargeback{
		hiveHost:   cfg.HiveHost,
		prestoHost: cfg.PrestoHost,
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	fmt.Println("setting up extensions client...")
	if cb.extension, err = ext_client.NewForConfig(config); err != nil {
		return nil, err
	}

	fmt.Println("setting up chargeback client...")
	if cb.charge, err = chargeback.NewForConfig(config); err != nil {
		return nil, err
	}

	cb.reportInform = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc:  cb.charge.Reports().List,
			WatchFunc: cb.charge.Reports().Watch,
		},
		&chargeback.Report{}, 3*time.Minute, cache.Indexers{},
	)

	fmt.Println("configuring event listeners")
	cb.reportInform.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: cb.handleAddReport,
	})

	fmt.Println("All set up!")
	return cb, nil
}

type Chargeback struct {
	extension *ext_client.Clientset
	charge    *chargeback.ChargebackClient

	reportInform cache.SharedIndexInformer

	hiveHost   string
	prestoHost string
}

func (c *Chargeback) Run() error {
	err := c.createResources()
	if err != nil {
		panic(err)
	}

	// TODO: implement polling
	time.Sleep(15 * time.Second)

	stopCh := make(<-chan struct{})
	go c.reportInform.Run(stopCh)

	fmt.Println("running")

	<-stopCh
	return nil
}

func (c *Chargeback) createResources() error {
	cdrClient := c.extension.CustomResourceDefinitions()
	for _, cdr := range chargeback.Resources {
		if _, err := cdrClient.Create(cdr); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

func (c *Chargeback) hiveConn() (*hive.Connection, error) {
	hive, err := hive.Connect(c.hiveHost)
	if err != nil {
		return nil, err
	}
	return hive, nil
}

func (c *Chargeback) prestoConn() (*sql.DB, error) {
	connStr := fmt.Sprintf("presto://%s/hive/default", c.prestoHost)
	db, err := sql.Open("prestgo", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to presto: %v", err)
	}
	return db, nil
}