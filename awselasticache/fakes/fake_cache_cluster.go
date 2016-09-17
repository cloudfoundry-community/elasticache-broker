package fakes

import (
	"github.com/cloudfoundry-community/elasticache-broker/awselasticache"
)

type FakeCacheCluster struct {
	DescribeCalled           bool
	DescribeID               string
	DescribeCacheClusterDetails awselasticache.CacheClusterDetails
	DescribeError            error

	CreateCalled           bool
	CreateID               string
	CreateCacheClusterDetails awselasticache.CacheClusterDetails
	CreateError            error

	ModifyCalled           bool
	ModifyID               string
	ModifyCacheClusterDetails awselasticache.CacheClusterDetails
	ModifyApplyImmediately bool
	ModifyError            error

	DeleteCalled            bool
	DeleteID                string
	DeleteError             error
}

func (f *FakeCacheCluster) Describe(ID string) (awselasticache.CacheClusterDetails, error) {
	f.DescribeCalled = true
	f.DescribeID = ID

	return f.DescribeCacheClusterDetails, f.DescribeError
}

func (f *FakeCacheCluster) Create(ID string, cacheClusterDetails awselasticache.CacheClusterDetails) error {
	f.CreateCalled = true
	f.CreateID = ID
	f.CreateCacheClusterDetails = cacheClusterDetails

	return f.CreateError
}

func (f *FakeCacheCluster) Modify(ID string, cacheClusterDetails awselasticache.CacheClusterDetails, applyImmediately bool) error {
	f.ModifyCalled = true
	f.ModifyID = ID
	f.ModifyCacheClusterDetails = cacheClusterDetails
	f.ModifyApplyImmediately = applyImmediately

	return f.ModifyError
}

func (f *FakeCacheCluster) Delete(ID string) error {
	f.DeleteCalled = true
	f.DeleteID = ID

	return f.DeleteError
}
