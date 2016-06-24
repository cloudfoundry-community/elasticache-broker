package awselasticache

import (
	"errors"
)

type CacheCluster interface {
	Describe(ID string) (CacheClusterDetails, error)
	Create(ID string, cacheClusterDetails CacheClusterDetails) error
	Modify(ID string, cacheClusterDetails CacheClusterDetails, applyImmediately bool) error
	Delete(ID string) error
}

type CacheClusterDetails struct {
	CacheClusterId              string
	Status                      string
	Endpoint                    string
	Engine                      string
	EngineVersion               string
	CacheInstanceClass	        string
	Port                        int64
	NumCacheNodes               int64
	CacheSecurityGroups         []string
	CacheSubnetGroupName        string
	Tags                        map[string]string
}

var (
	ErrCacheClusterDoesNotExist = errors.New("elasticache cluster does not exist")
)
