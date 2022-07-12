package genesyscloud

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/mypurecloud/platform-client-sdk-go/v74/platformclientv2"
	"github.com/mypurecloud/terraform-provider-genesyscloud/genesyscloud/consistency_checker"
	"github.com/mypurecloud/terraform-provider-genesyscloud/genesyscloud/util/resourcedata"
	"github.com/mypurecloud/terraform-provider-genesyscloud/genesyscloud/util/stringmap"
	"github.com/mypurecloud/terraform-provider-genesyscloud/genesyscloud/util/typeconv"
)

var (
	journeyActionMapSchema = map[string]*schema.Schema{
		"is_active": {
			Description: "Whether the action map is active.",
			Type:        schema.TypeBool,
			Optional:    true,
			Default:     true,
		},
		"display_name": {
			Description: "Display name of the action map.",
			Type:        schema.TypeString,
			Required:    true,
		},
		"trigger_with_segments": {
			Description: "Trigger action map if any segment in the list is assigned to a given customer.",
			Type:        schema.TypeSet,
			Required:    true,
			Elem:        &schema.Schema{Type: schema.TypeString},
		},
		"trigger_with_event_conditions": {
			Description: "List of event conditions that must be satisfied to trigger the action map.",
			Type:        schema.TypeSet,
			Optional:    true,
			Elem:        eventConditionResource,
		},
		"trigger_with_outcome_probability_conditions": {
			Description: "Probability conditions for outcomes that must be satisfied to trigger the action map.",
			Type:        schema.TypeSet,
			Optional:    true,
			Elem:        outcomeProbabilityConditionResource,
		},
		"page_url_conditions": {
			Description: "URL conditions that a page must match for web actions to be displayable.",
			Type:        schema.TypeSet,
			Optional:    true,
			Elem:        urlConditionResource,
		},
		"activation": {
			Description: "Type of activation.",
			Type:        schema.TypeSet,
			Required:    true,
			MaxItems:    1,
			Elem:        activationResource,
		},
		"weight": {
			Description:  "Weight of the action map with higher number denoting higher weight. Low=1, Medium=2, High=3.",
			Type:         schema.TypeInt,
			Optional:     true,
			Default:      2,
			ValidateFunc: validation.IntBetween(1, 3),
		},
		"action": {
			Description: "The action that will be executed if this action map is triggered.",
			Type:        schema.TypeSet,
			Required:    true,
			MaxItems:    1,
			Elem:        actionMapActionResource,
		},
		"action_map_schedule_groups": {
			Description: "The action map's associated schedule groups.",
			Type:        schema.TypeSet,
			Optional:    true,
			MaxItems:    1,
			Elem:        actionMapScheduleGroupsResource,
		},
		"ignore_frequency_cap": {
			Description: "Override organization-level frequency cap and always offer web engagements from this action map.",
			Type:        schema.TypeBool,
			Optional:    true,
			Default:     false,
		},
		"start_date": {
			Description:      "Timestamp at which the action map is scheduled to start firing. Date time is represented as an ISO-8601 string without a timezone. For example: 2006-01-02T15:04:05.000000.",
			Type:             schema.TypeString,
			Required:         true, // Now is the default value for this field. Better to make it required.
			ValidateDiagFunc: validateLocalDateTimes,
		},
		"end_date": {
			Description:      "Timestamp at which the action map is scheduled to stop firing. Date time is represented as an ISO-8601 string without a timezone. For example: 2006-01-02T15:04:05.000000.",
			Type:             schema.TypeString,
			Optional:         true,
			ValidateDiagFunc: validateLocalDateTimes,
		},
	}

	eventConditionResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"key": {
				Description: "The event key.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"values": {
				Description: "The event values.",
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
			},
			"operator": {
				Description:  "The comparison operator. Valid values: containsAll, containsAny, notContainsAll, notContainsAny, equal, notEqual, greaterThan, greaterThanOrEqual, lessThan, lessThanOrEqual, startsWith, endsWith.",
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "equal",
				ValidateFunc: validation.StringInSlice([]string{"containsAll", "containsAny", "notContainsAll", "notContainsAny", "equal", "notEqual", "greaterThan", "greaterThanOrEqual", "lessThan", "lessThanOrEqual", "startsWith", "endsWith"}, false),
			},
			"stream_type": {
				Description:  "The stream type for which this condition can be satisfied. Valid values: Web, Custom, Conversation.",
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringInSlice([]string{"Web", "Custom", "Conversation"}, false),
			},
			"session_type": {
				Description: "The session type for which this condition can be satisfied.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"event_name": {
				Description: "The name of the event for which this condition can be satisfied.",
				Type:        schema.TypeString,
				Optional:    true,
			},
		},
	}

	outcomeProbabilityConditionResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"outcome_id": {
				Description: "The outcome ID.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"maximum_probability": {
				Description: "Probability value for the selected outcome at or above which the action map will trigger.",
				Type:        schema.TypeFloat,
				Required:    true,
			},
			"probability": {
				Description: "Additional probability condition, where if set, the action map will trigger if the current outcome probability is lower or equal to the value.",
				Type:        schema.TypeFloat,
				Optional:    true,
			},
		},
	}

	urlConditionResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"values": {
				Description: "The URL condition value.",
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
			},
			"operator": {
				Description:  "The comparison operator. Valid values: containsAll, containsAny, notContainsAll, notContainsAny, equal, notEqual, greaterThan, greaterThanOrEqual, lessThan, lessThanOrEqual, startsWith, endsWith.",
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringInSlice([]string{"containsAll", "containsAny", "notContainsAll", "notContainsAny", "equal", "notEqual", "greaterThan", "greaterThanOrEqual", "lessThan", "lessThanOrEqual", "startsWith", "endsWith"}, false),
			},
		},
	}

	activationResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"type": {
				Description:  "Type of activation. Valid values: immediate, on-next-visit, on-next-session, delay.",
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringInSlice([]string{"immediate", "on-next-visit", "on-next-session", "delay"}, false),
			},
			"delay_in_seconds": {
				Description: "Activation delay time amount.",
				Type:        schema.TypeInt,
				Optional:    true,
			},
		},
	}

	actionMapActionResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"action_template_id": {
				Description: "Action template associated with the action map. For media type contentOffer.",
				Type:        schema.TypeString,
				Optional:    true,
			},
			"media_type": {
				Description:  "Media type of action. Valid values: webchat, webMessagingOffer, contentOffer, architectFlow, openAction.",
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true, // Currently it is mandatory because of broken null handling for optional fields in API (GPE-11801)
				ValidateFunc: validation.StringInSlice([]string{"webchat", "webMessagingOffer", "contentOffer", "architectFlow", "openAction"}, false),
			},
			"architect_flow_fields": {
				Description: "Architect Flow Id and input contract. For media type architectFlow.",
				Type:        schema.TypeSet,
				Optional:    true,
				MaxItems:    1,
				Elem:        architectFlowFieldsResource,
			},
			"web_messaging_offer_fields": {
				Description: "Admin-configurable fields of a web messaging offer action. For media type webMessagingOffer.",
				Type:        schema.TypeSet,
				Optional:    true,
				MaxItems:    1,
				Elem:        webMessagingOfferFieldsResource,
			},
			"open_action_fields": {
				Description: "Admin-configurable fields of an open action. For media type openAction.",
				Type:        schema.TypeSet,
				Optional:    true,
				MaxItems:    1,
				Elem:        openActionFieldsResource,
			},
		},
	}

	architectFlowFieldsResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"architect_flow_id": {
				Description: "The architect flow.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"flow_request_mappings": {
				Description: "Collection of Architect Flow Request Mappings to use.",
				Type:        schema.TypeSet,
				Optional:    true,
				Elem:        requestMappingResource,
			},
		},
	}

	requestMappingResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name": {
				Description: "Name of the Integration Action Attribute to supply the value for",
				Type:        schema.TypeString,
				Required:    true,
			},
			"attribute_type": {
				Description:  "Type of the value supplied. Valid values: String, Number, Integer, Boolean.",
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringInSlice([]string{"String", "Number", "Integer", "Boolean"}, false),
			},
			"mapping_type": {
				Description:  "Method of finding value to use with Attribute. Valid values: Lookup, HardCoded.",
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringInSlice([]string{"Lookup", "HardCoded"}, false),
			},
			"value": {
				Description: "Value to supply for the specified Attribute",
				Type:        schema.TypeString,
				Required:    true,
			},
		},
	}

	webMessagingOfferFieldsResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"offer_text": {
				Description: "Text value to be used when inviting a visitor to engage with a web messaging offer.",
				Type:        schema.TypeString,
				Optional:    true,
			},
			"architect_flow_id": {
				Description: "Flow to be invoked, overrides default flow when specified.",
				Type:        schema.TypeString,
				Optional:    true,
			},
		},
	}

	openActionFieldsResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"open_action": {
				Description: "The specific type of the open action.",
				Type:        schema.TypeSet,
				Required:    true,
				MaxItems:    1,
				Elem:        domainEntityRefResource,
			},
			"configuration_fields": {
				Description:      "Custom fields defined in the schema referenced by the open action type selected.",
				Type:             schema.TypeString,
				Optional:         true,
				DiffSuppressFunc: suppressEquivalentJsonDiffs,
			},
		},
	}

	domainEntityRefResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"id": {
				Description: "Id.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"name": {
				Description: "Name.",
				Type:        schema.TypeString,
				Required:    true,
			},
		},
	}

	actionMapScheduleGroupsResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"action_map_schedule_group_id": {
				Description: "The actions map's associated schedule group.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"emergency_action_map_schedule_group_id": {
				Description: "The action map's associated emergency schedule group.",
				Type:        schema.TypeString,
				Optional:    true,
			},
		},
	}
)

func getAllJourneyActionMaps(_ context.Context, clientConfig *platformclientv2.Configuration) (ResourceIDMetaMap, diag.Diagnostics) {
	resources := make(ResourceIDMetaMap)
	journeyApi := platformclientv2.NewJourneyApiWithConfig(clientConfig)

	pageCount := 1 // Needed because of broken journey common paging
	for pageNum := 1; pageNum <= pageCount; pageNum++ {
		const pageSize = 100
		actionMaps, _, getErr := journeyApi.GetJourneyActionmaps(pageNum, pageSize, "", "", "", nil, nil, "")
		if getErr != nil {
			return nil, diag.Errorf("Failed to get page of journey action maps: %v", getErr)
		}

		if actionMaps.Entities == nil || len(*actionMaps.Entities) == 0 {
			break
		}

		for _, actionMap := range *actionMaps.Entities {
			resources[*actionMap.Id] = &ResourceMeta{Name: *actionMap.DisplayName}
		}

		pageCount = *actionMaps.PageCount
	}

	return resources, nil
}

func journeyActionMapExporter() *ResourceExporter {
	return &ResourceExporter{
		GetResourcesFunc: getAllWithPooledClient(getAllJourneyActionMaps),
		RefAttrs:         map[string]*RefAttrSettings{}, // No references
	}
}

func resourceJourneyActionMap() *schema.Resource {
	return &schema.Resource{
		Description:   "Genesys Cloud Journey Action Map",
		CreateContext: createWithPooledClient(createJourneyActionMap),
		ReadContext:   readWithPooledClient(readJourneyActionMap),
		UpdateContext: updateWithPooledClient(updateJourneyActionMap),
		DeleteContext: deleteWithPooledClient(deleteJourneyActionMap),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		SchemaVersion: 1,
		Schema:        journeyActionMapSchema,
	}
}

func createJourneyActionMap(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	sdkConfig := meta.(*providerMeta).ClientConfig
	journeyApi := platformclientv2.NewJourneyApiWithConfig(sdkConfig)
	actionMap := buildSdkActionMap(d)

	log.Printf("Creating journey action map %s", *actionMap.DisplayName)
	result, resp, err := journeyApi.PostJourneyActionmaps(*actionMap)
	if err != nil {
		input, _ := interfaceToJson(*actionMap)
		return diag.Errorf("failed to create journey action map %s: %s\n(input: %+v)\n(resp: %s)", *actionMap.DisplayName, err, input, getBody(resp))
	}

	d.SetId(*result.Id)

	log.Printf("Created journey action map %s %s", *result.DisplayName, *result.Id)
	return readJourneyActionMap(ctx, d, meta)
}

func readJourneyActionMap(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	sdkConfig := meta.(*providerMeta).ClientConfig
	journeyApi := platformclientv2.NewJourneyApiWithConfig(sdkConfig)

	log.Printf("Reading journey action map %s", d.Id())
	return withRetriesForRead(ctx, d, func() *resource.RetryError {
		actionMap, resp, getErr := journeyApi.GetJourneyActionmap(d.Id())
		if getErr != nil {
			if isStatus404(resp) {
				return resource.RetryableError(fmt.Errorf("failed to read journey action map %s: %s", d.Id(), getErr))
			}
			return resource.NonRetryableError(fmt.Errorf("failed to read journey action map %s: %s", d.Id(), getErr))
		}

		cc := consistency_checker.NewConsistencyCheck(ctx, d, meta, resourceJourneyActionMap())
		flattenActionMap(d, actionMap)

		log.Printf("Read journey action map %s %s", d.Id(), *actionMap.DisplayName)
		return cc.CheckState()
	})
}

func updateJourneyActionMap(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	sdkConfig := meta.(*providerMeta).ClientConfig
	journeyApi := platformclientv2.NewJourneyApiWithConfig(sdkConfig)
	patchActionMap := buildSdkPatchActionMap(d)

	log.Printf("Updating journey action map %s", d.Id())
	diagErr := retryWhen(isVersionMismatch, func() (*platformclientv2.APIResponse, diag.Diagnostics) {
		// Get current journey action map version
		actionMap, resp, getErr := journeyApi.GetJourneyActionmap(d.Id())
		if getErr != nil {
			return resp, diag.Errorf("Failed to read current journey action map %s: %s", d.Id(), getErr)
		}

		patchActionMap.Version = actionMap.Version
		_, resp, patchErr := journeyApi.PatchJourneyActionmap(d.Id(), *patchActionMap)
		if patchErr != nil {
			input, _ := interfaceToJson(*patchActionMap)
			return resp, diag.Errorf("Error updating journey action map %s: %s\n(input: %+v)\n(resp: %s)", *patchActionMap.DisplayName, patchErr, input, getBody(resp))
		}
		return resp, nil
	})
	if diagErr != nil {
		return diagErr
	}

	log.Printf("Updated journey action map %s", d.Id())
	return readJourneyActionMap(ctx, d, meta)
}

func deleteJourneyActionMap(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	displayName := d.Get("display_name").(string)

	sdkConfig := meta.(*providerMeta).ClientConfig
	journeyApi := platformclientv2.NewJourneyApiWithConfig(sdkConfig)

	log.Printf("Deleting journey action map with display name %s", displayName)
	if _, err := journeyApi.DeleteJourneyActionmap(d.Id()); err != nil {
		return diag.Errorf("Failed to delete journey action map with display name %s: %s", displayName, err)
	}

	return withRetries(ctx, 30*time.Second, func() *resource.RetryError {
		_, resp, err := journeyApi.GetJourneyActionmap(d.Id())
		if err != nil {
			if isStatus404(resp) {
				// journey action map deleted
				log.Printf("Deleted journey action map %s", d.Id())
				return nil
			}
			return resource.NonRetryableError(fmt.Errorf("error deleting journey action map %s: %s", d.Id(), err))
		}

		return resource.RetryableError(fmt.Errorf("journey action map %s still exists", d.Id()))
	})
}

func flattenActionMap(d *schema.ResourceData, actionMap *platformclientv2.Actionmap) {
	d.Set("is_active", *actionMap.IsActive)
	d.Set("display_name", *actionMap.DisplayName)
	d.Set("trigger_with_segments", stringListToSet(*actionMap.TriggerWithSegments))
	resourcedata.SetNillableValue(d, "trigger_with_event_conditions", flattenList(actionMap.TriggerWithEventConditions, flattenEventCondition))
	resourcedata.SetNillableValue(d, "trigger_with_outcome_probability_conditions", flattenList(actionMap.TriggerWithOutcomeProbabilityConditions, flattenOutcomeProbabilityCondition))
	resourcedata.SetNillableValue(d, "page_url_conditions", flattenList(actionMap.PageUrlConditions, flattenUrlCondition))
	d.Set("activation", flattenAsList(actionMap.Activation, flattenActivation))
	d.Set("weight", *actionMap.Weight)
	resourcedata.SetNillableValue(d, "action", flattenAsList(actionMap.Action, flattenActionMapAction))
	resourcedata.SetNillableValue(d, "action_map_schedule_groups", flattenAsList(actionMap.ActionMapScheduleGroups, flattenActionMapScheduleGroups))
	d.Set("ignore_frequency_cap", *actionMap.IgnoreFrequencyCap)
	resourcedata.SetNillableTime(d, "start_date", actionMap.StartDate)
	resourcedata.SetNillableTime(d, "end_date", actionMap.EndDate)
}

func buildSdkActionMap(actionMap *schema.ResourceData) *platformclientv2.Actionmap {
	isActive := actionMap.Get("is_active").(bool)
	displayName := actionMap.Get("display_name").(string)
	triggerWithSegments := buildSdkStringList(actionMap, "trigger_with_segments")
	triggerWithEventConditions := nilToEmptyList(resourcedata.BuildSdkList(actionMap, "trigger_with_event_conditions", buildSdkEventCondition))
	triggerWithOutcomeProbabilityConditions := resourcedata.BuildSdkList(actionMap, "trigger_with_outcome_probability_conditions", buildSdkOutcomeProbabilityCondition)
	pageUrlConditions := nilToEmptyList(resourcedata.BuildSdkList(actionMap, "page_url_conditions", buildSdkUrlCondition))
	activation := resourcedata.BuildSdkListFirstElement(actionMap, "activation", buildSdkActivation, true)
	weight := actionMap.Get("weight").(int)
	action := resourcedata.BuildSdkListFirstElement(actionMap, "action", buildSdkActionMapAction, true)
	actionMapScheduleGroups := resourcedata.BuildSdkListFirstElement(actionMap, "action_map_schedule_groups", buildSdkActionMapScheduleGroups, true)
	ignoreFrequencyCap := actionMap.Get("ignore_frequency_cap").(bool)
	startDate := resourcedata.GetNillableTime(actionMap, "start_date")
	endDate := resourcedata.GetNillableTime(actionMap, "end_date")

	return &platformclientv2.Actionmap{
		IsActive:                                &isActive,
		DisplayName:                             &displayName,
		TriggerWithSegments:                     triggerWithSegments,
		TriggerWithEventConditions:              triggerWithEventConditions,
		TriggerWithOutcomeProbabilityConditions: triggerWithOutcomeProbabilityConditions,
		PageUrlConditions:                       pageUrlConditions,
		Activation:                              activation,
		Weight:                                  &weight,
		Action:                                  action,
		ActionMapScheduleGroups:                 actionMapScheduleGroups,
		IgnoreFrequencyCap:                      &ignoreFrequencyCap,
		StartDate:                               startDate,
		EndDate:                                 endDate,
	}
}

func buildSdkPatchActionMap(actionMap *schema.ResourceData) *platformclientv2.Patchactionmap {
	isActive := actionMap.Get("is_active").(bool)
	displayName := actionMap.Get("display_name").(string)
	triggerWithSegments := buildSdkStringList(actionMap, "trigger_with_segments")
	triggerWithEventConditions := nilToEmptyList(resourcedata.BuildSdkList(actionMap, "trigger_with_event_conditions", buildSdkEventCondition))
	triggerWithOutcomeProbabilityConditions := nilToEmptyList(resourcedata.BuildSdkList(actionMap, "trigger_with_outcome_probability_conditions", buildSdkOutcomeProbabilityCondition))
	pageUrlConditions := nilToEmptyList(resourcedata.BuildSdkList(actionMap, "page_url_conditions", buildSdkUrlCondition))
	activation := resourcedata.BuildSdkListFirstElement(actionMap, "activation", buildSdkActivation, true)
	weight := actionMap.Get("weight").(int)
	action := resourcedata.BuildSdkListFirstElement(actionMap, "action", buildSdkPatchAction, true)
	actionMapScheduleGroups := resourcedata.BuildSdkListFirstElement(actionMap, "action_map_schedule_groups", buildSdkPatchActionMapScheduleGroups, true)
	ignoreFrequencyCap := actionMap.Get("ignore_frequency_cap").(bool)
	startDate := resourcedata.GetNillableTime(actionMap, "start_date")
	endDate := resourcedata.GetNillableTime(actionMap, "end_date")

	return &platformclientv2.Patchactionmap{
		IsActive:                                &isActive,
		DisplayName:                             &displayName,
		TriggerWithSegments:                     triggerWithSegments,
		TriggerWithEventConditions:              triggerWithEventConditions,
		TriggerWithOutcomeProbabilityConditions: triggerWithOutcomeProbabilityConditions,
		PageUrlConditions:                       pageUrlConditions,
		Activation:                              activation,
		Weight:                                  &weight,
		Action:                                  action,
		ActionMapScheduleGroups:                 actionMapScheduleGroups,
		IgnoreFrequencyCap:                      &ignoreFrequencyCap,
		StartDate:                               startDate,
		EndDate:                                 endDate,
	}
}

func flattenEventCondition(eventCondition *platformclientv2.Eventcondition) map[string]interface{} {
	eventConditionMap := make(map[string]interface{})
	eventConditionMap["key"] = *eventCondition.Key
	eventConditionMap["values"] = stringListToSet(*eventCondition.Values)
	eventConditionMap["operator"] = *eventCondition.Operator
	eventConditionMap["stream_type"] = *eventCondition.StreamType
	eventConditionMap["session_type"] = *eventCondition.SessionType
	stringmap.SetValueIfNotNil(eventConditionMap, "event_name", eventCondition.EventName)
	return eventConditionMap
}

func buildSdkEventCondition(eventCondition map[string]interface{}) *platformclientv2.Eventcondition {
	key := eventCondition["key"].(string)
	values := stringmap.BuildSdkStringList(eventCondition, "values")
	operator := eventCondition["operator"].(string)
	streamType := eventCondition["stream_type"].(string)
	sessionType := eventCondition["session_type"].(string)
	eventName := stringmap.GetNonDefaultValue[string](eventCondition, "event_name")

	return &platformclientv2.Eventcondition{
		Key:         &key,
		Values:      values,
		Operator:    &operator,
		StreamType:  &streamType,
		SessionType: &sessionType,
		EventName:   eventName,
	}
}

func flattenOutcomeProbabilityCondition(outcomeProbabilityCondition *platformclientv2.Outcomeprobabilitycondition) map[string]interface{} {
	outcomeProbabilityConditionMap := make(map[string]interface{})
	outcomeProbabilityConditionMap["outcome_id"] = *outcomeProbabilityCondition.OutcomeId
	outcomeProbabilityConditionMap["maximum_probability"] = *typeconv.Float32to64(outcomeProbabilityCondition.MaximumProbability)
	stringmap.SetValueIfNotNil(outcomeProbabilityConditionMap, "probability", typeconv.Float32to64(outcomeProbabilityCondition.Probability))
	return outcomeProbabilityConditionMap
}

func buildSdkOutcomeProbabilityCondition(outcomeProbabilityCondition map[string]interface{}) *platformclientv2.Outcomeprobabilitycondition {
	outcomeId := outcomeProbabilityCondition["outcome_id"].(string)
	maximumProbability64 := outcomeProbabilityCondition["maximum_probability"].(float64)
	maximumProbability := typeconv.Float64to32(&maximumProbability64)
	probability := typeconv.Float64to32(stringmap.GetNonDefaultValue[float64](outcomeProbabilityCondition, "probability"))

	return &platformclientv2.Outcomeprobabilitycondition{
		OutcomeId:          &outcomeId,
		MaximumProbability: maximumProbability,
		Probability:        probability,
	}
}

func flattenUrlCondition(urlCondition *platformclientv2.Urlcondition) map[string]interface{} {
	urlConditionMap := make(map[string]interface{})
	urlConditionMap["values"] = stringListToSet(*urlCondition.Values)
	urlConditionMap["operator"] = *urlCondition.Operator
	return urlConditionMap
}

func buildSdkUrlCondition(eventCondition map[string]interface{}) *platformclientv2.Urlcondition {
	values := stringmap.BuildSdkStringList(eventCondition, "values")
	operator := eventCondition["operator"].(string)

	return &platformclientv2.Urlcondition{
		Values:   values,
		Operator: &operator,
	}
}

func flattenActivation(activation *platformclientv2.Activation) map[string]interface{} {
	activationMap := make(map[string]interface{})
	activationMap["type"] = *activation.VarType
	stringmap.SetValueIfNotNil(activationMap, "delay_in_seconds", activation.DelayInSeconds)
	return activationMap
}

func buildSdkActivation(activation map[string]interface{}) *platformclientv2.Activation {
	varType := activation["type"].(string)
	delayInSeconds := stringmap.GetNonDefaultValue[int](activation, "delay_in_seconds")

	return &platformclientv2.Activation{
		VarType:        &varType,
		DelayInSeconds: delayInSeconds,
	}
}

func flattenActionMapAction(actionMapAction *platformclientv2.Actionmapaction) map[string]interface{} {
	actionMapActionMap := make(map[string]interface{})
	actionMapActionMap["media_type"] = *actionMapAction.MediaType
	if actionMapAction.ActionTemplate != nil {
		stringmap.SetValueIfNotNil(actionMapActionMap, "action_template_id", actionMapAction.ActionTemplate.Id)
	}
	stringmap.SetValueIfNotNil(actionMapActionMap, "architect_flow_fields", flattenAsList(actionMapAction.ArchitectFlowFields, flattenArchitectFlowFields))
	stringmap.SetValueIfNotNil(actionMapActionMap, "web_messaging_offer_fields", flattenAsList(actionMapAction.WebMessagingOfferFields, flattenWebMessagingOfferFields))
	stringmap.SetValueIfNotNil(actionMapActionMap, "open_action_fields", flattenAsList(actionMapAction.OpenActionFields, flattenOpenActionFields))
	return actionMapActionMap
}

func buildSdkActionMapAction(actionMapAction map[string]interface{}) *platformclientv2.Actionmapaction {
	mediaType := actionMapAction["media_type"].(string)
	actionMapActionTemplate := getActionMapActionTemplate(actionMapAction)
	architectFlowFields := stringmap.BuildSdkListFirstElement(actionMapAction, "architect_flow_fields", buildSdkArchitectFlowFields, true)
	webMessagingOfferFields := stringmap.BuildSdkListFirstElement(actionMapAction, "web_messaging_offer_fields", buildSdkWebMessagingOfferFields, true)
	openActionFields := stringmap.BuildSdkListFirstElement(actionMapAction, "open_action_fields", buildSdkOpenActionFields, true)

	return &platformclientv2.Actionmapaction{
		MediaType:               &mediaType,
		ActionTemplate:          actionMapActionTemplate,
		ArchitectFlowFields:     architectFlowFields,
		WebMessagingOfferFields: webMessagingOfferFields,
		OpenActionFields:        openActionFields,
	}
}

func buildSdkPatchAction(patchAction map[string]interface{}) *platformclientv2.Patchaction {
	mediaType := patchAction["media_type"].(string)
	actionMapActionTemplate := getActionMapActionTemplate(patchAction)
	architectFlowFields := stringmap.BuildSdkListFirstElement(patchAction, "architect_flow_fields", buildSdkArchitectFlowFields, true)
	webMessagingOfferFields := stringmap.BuildSdkListFirstElement(patchAction, "web_messaging_offer_fields", buildSdkWebMessagingOfferFields, true)
	openActionFields := stringmap.BuildSdkListFirstElement(patchAction, "open_action_fields", buildSdkOpenActionFields, true)

	return &platformclientv2.Patchaction{
		MediaType:               &mediaType,
		ActionTemplate:          actionMapActionTemplate,
		ArchitectFlowFields:     architectFlowFields,
		WebMessagingOfferFields: webMessagingOfferFields,
		OpenActionFields:        openActionFields,
	}
}

func getActionMapActionTemplate(actionMapAction map[string]interface{}) *platformclientv2.Actionmapactiontemplate {
	actionMapActionTemplateId := stringmap.GetNonDefaultValue[string](actionMapAction, "action_template_id")
	var actionMapActionTemplate *platformclientv2.Actionmapactiontemplate = nil
	if actionMapActionTemplateId != nil {
		actionMapActionTemplate = &platformclientv2.Actionmapactiontemplate{
			Id: actionMapActionTemplateId,
		}
	}
	return actionMapActionTemplate
}

func flattenArchitectFlowFields(architectFlowFields *platformclientv2.Architectflowfields) map[string]interface{} {
	architectFlowFieldsMap := make(map[string]interface{})
	architectFlowFieldsMap["architect_flow_id"] = *architectFlowFields.ArchitectFlow.Id
	stringmap.SetValueIfNotNil(architectFlowFieldsMap, "flow_request_mappings", flattenList(architectFlowFields.FlowRequestMappings, flattenRequestMapping))
	return architectFlowFieldsMap
}

func buildSdkArchitectFlowFields(architectFlowFields map[string]interface{}) *platformclientv2.Architectflowfields {
	architectFlow := getArchitectFlow(architectFlowFields)
	flowRequestMappings := stringmap.BuildSdkList(architectFlowFields, "flow_request_mappings", buildSdkRequestMapping)

	return &platformclientv2.Architectflowfields{
		ArchitectFlow:       architectFlow,
		FlowRequestMappings: flowRequestMappings,
	}
}

func flattenRequestMapping(requestMapping *platformclientv2.Requestmapping) map[string]interface{} {
	requestMappingMap := make(map[string]interface{})
	requestMappingMap["name"] = *requestMapping.Name
	requestMappingMap["attribute_type"] = *requestMapping.AttributeType
	requestMappingMap["mapping_type"] = *requestMapping.MappingType
	requestMappingMap["value"] = *requestMapping.Value
	return requestMappingMap
}

func buildSdkRequestMapping(RequestMapping map[string]interface{}) *platformclientv2.Requestmapping {
	name := RequestMapping["name"].(string)
	attributeType := RequestMapping["attribute_type"].(string)
	mappingType := RequestMapping["mapping_type"].(string)
	value := RequestMapping["value"].(string)

	return &platformclientv2.Requestmapping{
		Name:          &name,
		AttributeType: &attributeType,
		MappingType:   &mappingType,
		Value:         &value,
	}
}

func flattenWebMessagingOfferFields(webMessagingOfferFields *platformclientv2.Webmessagingofferfields) map[string]interface{} {
	webMessagingOfferFieldsMap := make(map[string]interface{})
	if webMessagingOfferFields.OfferText == nil && (webMessagingOfferFields.ArchitectFlow == nil || webMessagingOfferFields.ArchitectFlow.Id == nil) {
		return nil
	}
	stringmap.SetValueIfNotNil(webMessagingOfferFieldsMap, "offer_text", webMessagingOfferFields.OfferText)
	if webMessagingOfferFields.ArchitectFlow != nil {
		stringmap.SetValueIfNotNil(webMessagingOfferFieldsMap, "architect_flow_id", webMessagingOfferFields.ArchitectFlow.Id)
	}
	return webMessagingOfferFieldsMap
}

func buildSdkWebMessagingOfferFields(webMessagingOfferFields map[string]interface{}) *platformclientv2.Webmessagingofferfields {
	offerText := stringmap.GetNonDefaultValue[string](webMessagingOfferFields, "offer_text")
	architectFlow := getArchitectFlow(webMessagingOfferFields)

	return &platformclientv2.Webmessagingofferfields{
		OfferText:     offerText,
		ArchitectFlow: architectFlow,
	}
}

func getArchitectFlow(actionMapAction map[string]interface{}) *platformclientv2.Addressableentityref {
	architectFlowId := stringmap.GetNonDefaultValue[string](actionMapAction, "architect_flow_id")
	var architectFlow *platformclientv2.Addressableentityref = nil
	if architectFlowId != nil {
		architectFlow = &platformclientv2.Addressableentityref{
			Id: architectFlowId,
		}
	}
	return architectFlow
}

func flattenOpenActionFields(openActionFields *platformclientv2.Openactionfields) map[string]interface{} {
	architectFlowFieldsMap := make(map[string]interface{})
	architectFlowFieldsMap["open_action"] = flattenAsList(openActionFields.OpenAction, flattenOpenActionDomainEntityRef)
	if openActionFields.ConfigurationFields != nil {
		jsonString, err := interfaceToJson(openActionFields.ConfigurationFields)
		if err != nil {
			log.Printf("Error marshalling '%s': %v", "configuration_fields", err)
		}
		architectFlowFieldsMap["configuration_fields"] = jsonString
	}
	return architectFlowFieldsMap
}

func buildSdkOpenActionFields(openActionFieldsMap map[string]interface{}) *platformclientv2.Openactionfields {
	openAction := stringmap.BuildSdkListFirstElement(openActionFieldsMap, "open_action", buildSdkOpenActionDomainEntityRef, true)
	openActionFields := platformclientv2.Openactionfields{
		OpenAction: openAction,
	}

	configurationFieldsString := stringmap.GetNonDefaultValue[string](openActionFieldsMap, "configuration_fields")
	if configurationFieldsString != nil {
		configurationFieldsInterface, err := jsonStringToInterface(*configurationFieldsString)
		if err != nil {
			log.Printf("Error unmarshalling '%s': %v", "configuration_fields", err)
		}
		configurationFieldsMap := configurationFieldsInterface.(map[string]interface{})
		openActionFields.ConfigurationFields = &configurationFieldsMap
	}

	return &openActionFields
}

func flattenOpenActionDomainEntityRef(domainEntityRef *platformclientv2.Domainentityref) map[string]interface{} {
	domainEntityRefMap := make(map[string]interface{})
	domainEntityRefMap["id"] = *domainEntityRef.Id
	domainEntityRefMap["name"] = *domainEntityRef.Name
	return domainEntityRefMap
}

func buildSdkOpenActionDomainEntityRef(domainEntityRef map[string]interface{}) *platformclientv2.Domainentityref {
	id := domainEntityRef["id"].(string)
	name := domainEntityRef["name"].(string)

	return &platformclientv2.Domainentityref{
		Id:   &id,
		Name: &name,
	}
}

func flattenActionMapScheduleGroups(actionMapScheduleGroups *platformclientv2.Actionmapschedulegroups) map[string]interface{} {
	actionMapScheduleGroupsMap := make(map[string]interface{})
	actionMapScheduleGroupsMap["action_map_schedule_group_id"] = *actionMapScheduleGroups.ActionMapScheduleGroup.Id
	if actionMapScheduleGroups.EmergencyActionMapScheduleGroup != nil {
		stringmap.SetValueIfNotNil(actionMapScheduleGroupsMap, "emergency_action_map_schedule_group_id", actionMapScheduleGroups.EmergencyActionMapScheduleGroup.Id)
	}
	return actionMapScheduleGroupsMap
}

func buildSdkActionMapScheduleGroups(actionMapScheduleGroups map[string]interface{}) *platformclientv2.Actionmapschedulegroups {
	actionMapScheduleGroup, emergencyActionMapScheduleGroup := getActionMapScheduleGroupPair(actionMapScheduleGroups)

	return &platformclientv2.Actionmapschedulegroups{
		ActionMapScheduleGroup:          actionMapScheduleGroup,
		EmergencyActionMapScheduleGroup: emergencyActionMapScheduleGroup,
	}
}

func buildSdkPatchActionMapScheduleGroups(actionMapScheduleGroups map[string]interface{}) *platformclientv2.Patchactionmapschedulegroups {
	if actionMapScheduleGroups == nil {
		return nil
	}

	actionMapScheduleGroup, emergencyActionMapScheduleGroup := getActionMapScheduleGroupPair(actionMapScheduleGroups)

	return &platformclientv2.Patchactionmapschedulegroups{
		ActionMapScheduleGroup:          actionMapScheduleGroup,
		EmergencyActionMapScheduleGroup: emergencyActionMapScheduleGroup,
	}
}

func getActionMapScheduleGroupPair(actionMapScheduleGroups map[string]interface{}) (*platformclientv2.Actionmapschedulegroup, *platformclientv2.Actionmapschedulegroup) {
	actionMapScheduleGroupId := actionMapScheduleGroups["action_map_schedule_group_id"].(string)
	actionMapScheduleGroup := &platformclientv2.Actionmapschedulegroup{
		Id: &actionMapScheduleGroupId,
	}
	emergencyActionMapScheduleGroupId := stringmap.GetNonDefaultValue[string](actionMapScheduleGroups, "emergency_action_map_schedule_group_id")
	var emergencyActionMapScheduleGroup *platformclientv2.Actionmapschedulegroup = nil
	if emergencyActionMapScheduleGroupId != nil {
		emergencyActionMapScheduleGroup = &platformclientv2.Actionmapschedulegroup{
			Id: emergencyActionMapScheduleGroupId,
		}
	}
	return actionMapScheduleGroup, emergencyActionMapScheduleGroup
}
