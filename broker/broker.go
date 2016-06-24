package broker

import (
	"encoding/json"
//	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/frodenas/brokerapi"
	"github.com/mitchellh/mapstructure"
	"github.com/pivotal-golang/lager"

	"github.com/apefactory/elasticache-broker/awselasticache"
)

const instanceIDLogKey = "instance-id"
const bindingIDLogKey = "binding-id"
const detailsLogKey = "details"
const acceptsIncompleteLogKey = "acceptsIncomplete"

var elastiCacheStatus2State = map[string]string{
	"available":                      brokerapi.LastOperationSucceeded,
	"backing-up":                     brokerapi.LastOperationInProgress,
	"creating":                       brokerapi.LastOperationInProgress,
	"deleting":                       brokerapi.LastOperationInProgress,
	"deleted":                        brokerapi.LastOperationInProgress,
	"incompatible-network":           brokerapi.LastOperationInProgress,
	"modifying":                    	brokerapi.LastOperationInProgress,
	"rebooting cache cluster nodes,": brokerapi.LastOperationInProgress,
	"restore-failed":                 brokerapi.LastOperationInProgress,
	"snapshotting": 									brokerapi.LastOperationInProgress,
}

type ElastiCacheBroker struct {
	cachePrefix                  string
	allowUserProvisionParameters bool
	allowUserUpdateParameters    bool
	allowUserBindParameters      bool
	catalog                      Catalog
	cacheCluster                 awselasticache.CacheCluster
	logger                       lager.Logger
}

func New(
	config Config,
	cacheCluster awselasticache.CacheCluster,
	logger lager.Logger,
) *ElastiCacheBroker {
	return &ElastiCacheBroker{
		cachePrefix:                  config.CachePrefix,
		allowUserProvisionParameters: config.AllowUserProvisionParameters,
		allowUserUpdateParameters:    config.AllowUserUpdateParameters,
		catalog:                      config.Catalog,
		cacheCluster:                 cacheCluster,
		logger:                       logger.Session("broker"),
	}
}

func (b *ElastiCacheBroker) Services() brokerapi.CatalogResponse {
	catalogResponse := brokerapi.CatalogResponse{}

	brokerCatalog, err := json.Marshal(b.catalog)
	if err != nil {
		b.logger.Error("marshal-error", err)
		return catalogResponse
	}

	apiCatalog := brokerapi.Catalog{}
	if err = json.Unmarshal(brokerCatalog, &apiCatalog); err != nil {
		b.logger.Error("unmarshal-error", err)
		return catalogResponse
	}

	catalogResponse.Services = apiCatalog.Services

	return catalogResponse
}

func (b *ElastiCacheBroker) Provision(instanceID string, details brokerapi.ProvisionDetails, acceptsIncomplete bool) (brokerapi.ProvisioningResponse, bool, error) {
	b.logger.Debug("provision", lager.Data{
		instanceIDLogKey:        instanceID,
		detailsLogKey:           details,
		acceptsIncompleteLogKey: acceptsIncomplete,
	})

	provisioningResponse := brokerapi.ProvisioningResponse{}
	if !acceptsIncomplete {
		return provisioningResponse, false, brokerapi.ErrAsyncRequired
	}

	provisionParameters := ProvisionParameters{}
	if b.allowUserProvisionParameters {
		if err := mapstructure.Decode(details.Parameters, &provisionParameters); err != nil {
			return provisioningResponse, false, err
		}
	}

	servicePlan, ok := b.catalog.FindServicePlan(details.PlanID)
	if !ok {
		return provisioningResponse, false, fmt.Errorf("Service Plan '%s' not found", details.PlanID)
	}

	var err error
	instance := b.createCacheCluster(instanceID, servicePlan, provisionParameters, details)
	if err = b.cacheCluster.Create(b.cacheClusterIdentifier(instanceID), *instance); err != nil {
		return provisioningResponse, false, err
	}

	return provisioningResponse, true, nil
}

func (b *ElastiCacheBroker) Update(instanceID string, details brokerapi.UpdateDetails, acceptsIncomplete bool) (bool, error) {
	b.logger.Debug("update", lager.Data{
		instanceIDLogKey:        instanceID,
		detailsLogKey:           details,
		acceptsIncompleteLogKey: acceptsIncomplete,
	})

	if !acceptsIncomplete {
		return false, brokerapi.ErrAsyncRequired
	}

	updateParameters := UpdateParameters{}
	if b.allowUserUpdateParameters {
		if err := mapstructure.Decode(details.Parameters, &updateParameters); err != nil {
			return false, err
		}
	}

	service, ok := b.catalog.FindService(details.ServiceID)
	if !ok {
		return false, fmt.Errorf("Service '%s' not found", details.ServiceID)
	}

	if !service.PlanUpdateable {
		return false, brokerapi.ErrInstanceNotUpdateable
	}

	servicePlan, ok := b.catalog.FindServicePlan(details.PlanID)
	if !ok {
		return false, fmt.Errorf("Service Plan '%s' not found", details.PlanID)
	}

	instance := b.modifyCacheCluster(instanceID, servicePlan, updateParameters, details)
	if err := b.cacheCluster.Modify(b.cacheClusterIdentifier(instanceID), *instance, updateParameters.ApplyImmediately); err != nil {
		if err == awselasticache.ErrCacheClusterDoesNotExist {
			return false, brokerapi.ErrInstanceDoesNotExist
		}
		return false, err
	}

	return true, nil
}

func (b *ElastiCacheBroker) Deprovision(instanceID string, details brokerapi.DeprovisionDetails, acceptsIncomplete bool) (bool, error) {
	b.logger.Debug("deprovision", lager.Data{
		instanceIDLogKey:        instanceID,
		detailsLogKey:           details,
		acceptsIncompleteLogKey: acceptsIncomplete,
	})

	if !acceptsIncomplete {
		return false, brokerapi.ErrAsyncRequired
	}

	if err := b.cacheCluster.Delete(b.cacheClusterIdentifier(instanceID)); err != nil {
		if err == awselasticache.ErrCacheClusterDoesNotExist {
			return false, brokerapi.ErrInstanceDoesNotExist
		}
		return false, err
	}

	return true, nil
}

func (b *ElastiCacheBroker) Bind(instanceID, bindingID string, details brokerapi.BindDetails) (brokerapi.BindingResponse, error) {
	b.logger.Debug("bind", lager.Data{
		instanceIDLogKey: instanceID,
		bindingIDLogKey:  bindingID,
		detailsLogKey:    details,
	})

	bindingResponse := brokerapi.BindingResponse{}

	service, ok := b.catalog.FindService(details.ServiceID)
	if !ok {
		return bindingResponse, fmt.Errorf("Service '%s' not found", details.ServiceID)
	}

	if !service.Bindable {
		return bindingResponse, brokerapi.ErrInstanceNotBindable
	}

	var cacheEndpoint string
	var cachePort int64

	cacheClusterDetails, err := b.cacheCluster.Describe(b.cacheClusterIdentifier(instanceID))
	if err != nil {
		if err == awselasticache.ErrCacheClusterDoesNotExist {
			return bindingResponse, brokerapi.ErrInstanceDoesNotExist
		}
		return bindingResponse, err
	}

	cacheEndpoint = cacheClusterDetails.Endpoint
	cachePort = cacheClusterDetails.Port

	bindingResponse.Credentials = &brokerapi.CredentialsHash{
		Host:     cacheEndpoint,
		Port:     cachePort,
		Name:     b.cacheClusterIdentifier(instanceID),
	}

	return bindingResponse, nil
}

func (b *ElastiCacheBroker) Unbind(instanceID, bindingID string, details brokerapi.UnbindDetails) error {
	b.logger.Debug("unbind", lager.Data{
		instanceIDLogKey: instanceID,
		bindingIDLogKey:  bindingID,
		detailsLogKey:    details,
	})

	return nil
}

func (b *ElastiCacheBroker) LastOperation(instanceID string) (brokerapi.LastOperationResponse, error) {
	b.logger.Debug("last-operation", lager.Data{
		instanceIDLogKey: instanceID,
	})

	lastOperationResponse := brokerapi.LastOperationResponse{State: brokerapi.LastOperationFailed}

	cacheClusterDetails, err := b.cacheCluster.Describe(b.cacheClusterIdentifier(instanceID))
	if err != nil {
		if err == awselasticache.ErrCacheClusterDoesNotExist {
			return lastOperationResponse, brokerapi.ErrInstanceDoesNotExist
		}
		return lastOperationResponse, err
	}

	lastOperationResponse.Description = fmt.Sprintf("Cache Cluster Instance '%s' status is '%s'", b.cacheClusterIdentifier(instanceID), cacheClusterDetails.Status)

	if state, ok := elastiCacheStatus2State[cacheClusterDetails.Status]; ok {
		lastOperationResponse.State = state
	}

//	if lastOperationResponse.State == brokerapi.LastOperationSucceeded && cacheClusterDetails.PendingModifications {
//		lastOperationResponse.State = brokerapi.LastOperationInProgress
//		lastOperationResponse.Description = fmt.Sprintf("Cache Cluster Instance '%s' has pending modifications", b.cacheClusterIdentifier(instanceID))
//	}

	return lastOperationResponse, nil
}

func (b *ElastiCacheBroker) cacheClusterIdentifier(instanceID string) string {
	id := fmt.Sprintf("%s-%s", b.cachePrefix, strings.Replace(instanceID, "-", "", -1))[:20]
	return id
}

func (b *ElastiCacheBroker) createCacheCluster(instanceID string, servicePlan ServicePlan, provisionParameters ProvisionParameters, details brokerapi.ProvisionDetails) *awselasticache.CacheClusterDetails {
	cacheClusterDetails := b.cacheClusterFromPlan(servicePlan)


	cacheClusterDetails.Tags = b.cacheTags("Created", details.ServiceID, details.PlanID, details.OrganizationGUID, details.SpaceGUID)
	return cacheClusterDetails
}

func (b *ElastiCacheBroker) modifyCacheCluster(instanceID string, servicePlan ServicePlan, updateParameters UpdateParameters, details brokerapi.UpdateDetails) *awselasticache.CacheClusterDetails {
	cacheClusterDetails := b.cacheClusterFromPlan(servicePlan)


	cacheClusterDetails.Tags = b.cacheTags("Updated", details.ServiceID, details.PlanID, "", "")
	return cacheClusterDetails
}

func (b *ElastiCacheBroker) cacheClusterFromPlan(servicePlan ServicePlan) *awselasticache.CacheClusterDetails {
	cacheClusterDetails := &awselasticache.CacheClusterDetails{
		Engine: servicePlan.ElastiCacheProperties.Engine,
	}
	if servicePlan.ElastiCacheProperties.EngineVersion != "" {
		cacheClusterDetails.EngineVersion = servicePlan.ElastiCacheProperties.EngineVersion
	}
	if servicePlan.ElastiCacheProperties.Port > 0 {
		cacheClusterDetails.Port = servicePlan.ElastiCacheProperties.Port
	}
	if servicePlan.ElastiCacheProperties.NumCacheNodes > 0 {
		cacheClusterDetails.NumCacheNodes = servicePlan.ElastiCacheProperties.NumCacheNodes
	}
	if servicePlan.ElastiCacheProperties.CacheInstanceClass != "" {
		cacheClusterDetails.CacheInstanceClass = servicePlan.ElastiCacheProperties.CacheInstanceClass
	}
	if servicePlan.ElastiCacheProperties.CacheSubnetGroupName != "" {
		cacheClusterDetails.CacheSubnetGroupName = servicePlan.ElastiCacheProperties.CacheSubnetGroupName
	}
	if len(servicePlan.ElastiCacheProperties.CacheSecurityGroups) > 0 {
		cacheClusterDetails.CacheSecurityGroups = servicePlan.ElastiCacheProperties.CacheSecurityGroups
	}

	return cacheClusterDetails
}

func (b *ElastiCacheBroker) cacheTags(action, serviceID, planID, organizationID, spaceID string) map[string]string {
	tags := make(map[string]string)

	tags["Owner"] = "Cloud Foundry"

	tags[action+" by"] = "AWS ElastiCache Service Broker"

	tags[action+" at"] = time.Now().Format(time.RFC822Z)

	if serviceID != "" {
		tags["Service ID"] = serviceID
	}

	if planID != "" {
		tags["Plan ID"] = planID
	}

	if organizationID != "" {
		tags["Organization ID"] = organizationID
	}

	if spaceID != "" {
		tags["Space ID"] = spaceID
	}
	return tags
}
