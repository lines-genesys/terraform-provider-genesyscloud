package genesyscloud

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/MyPureCloud/platform-client-sdk-go/platformclientv2"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

var (
	mediaSettingsKeyCall     = "call"
	mediaSettingsKeyCallback = "callback"
	mediaSettingsKeyChat     = "chat"
	mediaSettingsKeyEmail    = "email"
	mediaSettingsKeyMessage  = "message"
	mediaSettingsKeySocial   = "socialExpression"
	mediaSettingsKeyVideo    = "videoComm"

	bullseyeExpansionTypeTimeout = "TIMEOUT_SECONDS"

	queueMediaSettingsResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"alerting_timeout_sec": {
				Description:  "Alerting timeout in seconds. Must be >= 7",
				Type:         schema.TypeInt,
				Required:     true,
				ValidateFunc: validation.IntAtLeast(7),
			},
			"service_level_percentage": {
				Description:  "The desired Service Level. A value between 0 and 1.",
				Type:         schema.TypeFloat,
				Required:     true,
				ValidateFunc: validation.FloatBetween(0, 1),
			},
			"service_level_duration_ms": {
				Description:  "Service Level target in milliseconds. Must be >= 1000",
				Type:         schema.TypeInt,
				Required:     true,
				ValidateFunc: validation.IntAtLeast(1000),
			},
		},
	}

	queueMemberResource = &schema.Resource{
		Schema: map[string]*schema.Schema{
			"user_id": {
				Description: "User ID",
				Type:        schema.TypeString,
				Required:    true,
			},
			"ring_num": {
				Description:  "Ring number between 1 and 6 for this user in the queue. Defaults to 1.",
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      1,
				ValidateFunc: validation.IntBetween(1, 6),
			},
		},
	}
)

func routingQueueResource() *schema.Resource {
	return &schema.Resource{
		Description: "Genesys Cloud Routing Queue",

		CreateContext: createQueue,
		ReadContext:   readQueue,
		UpdateContext: updateQueue,
		DeleteContext: deleteQueue,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Description: "Queue name.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"division_id": {
				Description: "The division to which this queue will belong. If not set, the home division will be used.",
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
			},
			"description": {
				Description: "Queue description.",
				Type:        schema.TypeString,
				Optional:    true,
			},
			"media_settings_call": {
				Description: "Call media settings.",
				Type:        schema.TypeList,
				MaxItems:    1,
				Optional:    true,
				Computed:    true,
				Elem:        queueMediaSettingsResource,
			},
			"media_settings_callback": {
				Description: "Callback media settings.",
				Type:        schema.TypeList,
				MaxItems:    1,
				Optional:    true,
				Computed:    true,
				Elem:        queueMediaSettingsResource,
			},
			"media_settings_chat": {
				Description: "Chat media settings.",
				Type:        schema.TypeList,
				MaxItems:    1,
				Optional:    true,
				Computed:    true,
				Elem:        queueMediaSettingsResource,
			},
			"media_settings_email": {
				Description: "Email media settings.",
				Type:        schema.TypeList,
				MaxItems:    1,
				Optional:    true,
				Computed:    true,
				Elem:        queueMediaSettingsResource,
			},
			"media_settings_message": {
				Description: "Message media settings.",
				Type:        schema.TypeList,
				MaxItems:    1,
				Optional:    true,
				Computed:    true,
				Elem:        queueMediaSettingsResource,
			},
			"media_settings_social": {
				Description: "Social media settings.",
				Type:        schema.TypeList,
				MaxItems:    1,
				Optional:    true,
				Computed:    true,
				Elem:        queueMediaSettingsResource,
			},
			"media_settings_video": {
				Description: "Video media settings.",
				Type:        schema.TypeList,
				MaxItems:    1,
				Optional:    true,
				Computed:    true,
				Elem:        queueMediaSettingsResource,
			},
			"routing_rules": {
				Description: "The routing rules for the queue, used for routing to known or preferred agents.",
				Type:        schema.TypeList,
				Optional:    true,
				MaxItems:    6,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"operator": {
							Description:  "Matching operator (MEETS_THRESHOLD | ANY). MEETS_THRESHOLD matches any agent with a score at or above the rule's threshold. ANY matches all specified agents, regardless of score.",
							Type:         schema.TypeString,
							Optional:     true,
							Default:      "MEETS_THRESHOLD",
							ValidateFunc: validation.StringInSlice([]string{"MEETS_THRESHOLD", "ANY"}, false),
						},
						"threshold": {
							Description: "Threshold required for routing attempt (generally an agent score). Ignored for operator ANY.",
							Type:        schema.TypeInt,
							Optional:    true,
						},
						"wait_seconds": {
							Description:  "Seconds to wait in this rule before moving to the next.",
							Type:         schema.TypeFloat,
							Optional:     true,
							Default:      5,
							ValidateFunc: validation.FloatBetween(2, 900),
						},
					},
				},
			},
			"bullseye_rings": {
				Description: "The bullseye ring settings for the queue.",
				Type:        schema.TypeList,
				Optional:    true,
				MaxItems:    6,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"expansion_timeout_seconds": {
							Description:  "Seconds to wait in this ring before moving to the next.",
							Type:         schema.TypeFloat,
							Required:     true,
							ValidateFunc: validation.FloatBetween(2, 900),
						},
						"skills_to_remove": {
							Description: "Skill IDs to remove on ring exit.",
							Type:        schema.TypeSet,
							Optional:    true,
							Elem:        &schema.Schema{Type: schema.TypeString},
						},
					},
				},
			},
			"acw_wrapup_prompt": {
				Description:  "This field controls how the UI prompts the agent for a wrapup (MANDATORY | OPTIONAL | MANDATORY_TIMEOUT | MANDATORY_FORCED_TIMEOUT | AGENT_REQUESTED). Defaults to MANDATORY_TIMEOUT.",
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "MANDATORY_TIMEOUT",
				ValidateFunc: validation.StringInSlice([]string{"MANDATORY", "OPTIONAL", "MANDATORY_TIMEOUT", "MANDATORY_FORCED_TIMEOUT", "AGENT_REQUESTED"}, false),
			},
			"acw_timeout_ms": {
				Description:  "The amount of time the agent can stay in ACW. Only set when ACW is MANDATORY_TIMEOUT, MANDATORY_FORCED_TIMEOUT or AGENT_REQUESTED.",
				Type:         schema.TypeInt,
				Optional:     true,
				Computed:     true, // Default may be set by server
				ValidateFunc: validation.IntBetween(1000, 86400000),
			},
			"skill_evaluation_method": {
				Description:  "The skill evaluation method to use when routing conversations (NONE | BEST | ALL). Defaults to ALL.",
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "ALL",
				ValidateFunc: validation.StringInSlice([]string{"NONE", "BEST", "ALL"}, false),
			},
			"queue_flow_id": {
				Description: "The in-queue flow ID to use for conversations waiting in queue.",
				Type:        schema.TypeString,
				Optional:    true,
			},
			"whisper_prompt_id": {
				Description: "The prompt ID used for whisper on the queue, if configured.",
				Type:        schema.TypeString,
				Optional:    true,
			},
			"auto_answer_only": {
				Description: "Specifies whether the configured whisper should play for all ACD calls, or only for those which are auto-answered. Defaults to true.",
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
			},
			"enable_transcription": {
				Description: "Indicates whether voice transcription is enabled for this queue. Defaults to false.",
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
			},
			"enable_manual_assignment": {
				Description: "Indicates whether manual assignment is enabled for this queue. Defaults to false.",
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
			},
			"calling_party_name": {
				Description: "The name to use for caller identification for outbound calls from this queue.",
				Type:        schema.TypeString,
				Optional:    true,
			},
			"calling_party_number": {
				Description: "The phone number to use for caller identification for outbound calls from this queue.",
				Type:        schema.TypeString,
				Optional:    true,
			},
			"default_script_ids": {
				Description:      "The default script IDs for each communication type. Communication types: (CALL | CALLBACK | CHAT | COBROWSE | EMAIL | MESSAGE | SOCIAL_EXPRESSION | VIDEO | SCREENSHARE)",
				Type:             schema.TypeMap,
				ValidateDiagFunc: validateMapCommTypes,
				Optional:         true,
				Elem:             &schema.Schema{Type: schema.TypeString},
			},
			"outbound_messaging_sms_address_id": {
				Description: "The unique ID of the outbound messaging SMS address for the queue.",
				Type:        schema.TypeString,
				Optional:    true,
			},
			"outbound_email_address": {
				Description: "The outbound email address settings for this queue.",
				Type:        schema.TypeList,
				MaxItems:    1,
				Optional:    true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"domain_id": {
							Description: "Unique ID of the email domain. e.g. \"test.example.com\"",
							Type:        schema.TypeString,
							Required:    true,
						},
						"route_id": {
							Description: "Unique ID of the email route.",
							Type:        schema.TypeString,
							Required:    true,
						},
					},
				},
			},
			"members": {
				Description: "Users in the queue. If not set, this resource will not manage queue membership.",
				Type:        schema.TypeSet,
				Optional:    true,
				Computed:    true,
				Elem:        queueMemberResource,
			},
		},
	}
}

func createQueue(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	name := d.Get("name").(string)
	divisionID := d.Get("division_id").(string)
	description := d.Get("description").(string)
	skillEvaluationMethod := d.Get("skill_evaluation_method").(string)
	autoAnswerOnly := d.Get("auto_answer_only").(bool)
	enableTranscription := d.Get("enable_transcription").(bool)
	enableManualAssignment := d.Get("enable_manual_assignment").(bool)
	callingPartyName := d.Get("calling_party_name").(string)
	callingPartyNumber := d.Get("calling_party_number").(string)

	routingAPI := platformclientv2.NewRoutingApi()

	createQueue := platformclientv2.Createqueuerequest{
		Name:                       &name,
		Description:                &description,
		MediaSettings:              buildSdkMediaSettings(d),
		RoutingRules:               buildSdkRoutingRules(d),
		Bullseye:                   buildSdkBullseyeSettings(d),
		AcwSettings:                buildSdkAcwSettings(d),
		SkillEvaluationMethod:      &skillEvaluationMethod,
		QueueFlow:                  buildSdkDomainEntityRef(d, "queue_flow_id"),
		WhisperPrompt:              buildSdkDomainEntityRef(d, "whisper_prompt_id"),
		AutoAnswerOnly:             &autoAnswerOnly,
		CallingPartyName:           &callingPartyName,
		CallingPartyNumber:         &callingPartyNumber,
		DefaultScripts:             buildSdkDefaultScriptsMap(d),
		OutboundMessagingAddresses: buildSdkQueueMessagingAddresses(d),
		OutboundEmailAddress:       buildSdkQueueEmailAddress(d),
		EnableTranscription:        &enableTranscription,
		EnableManualAssignment:     &enableManualAssignment,
	}

	if divisionID != "" {
		createQueue.Division = &platformclientv2.Writabledivision{Id: &divisionID}
	}

	log.Printf("Creating queue %s", name)
	queue, _, err := routingAPI.PostRoutingQueues(createQueue)
	if err != nil {
		return diag.Errorf("Failed to create queue %s: %s", name, err)
	}
	d.SetId(*queue.Id)

	diagErr := updateQueueMembers(d)
	if diagErr != nil {
		return diagErr
	}
	return readQueue(ctx, d, meta)
}

func readQueue(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	routingAPI := platformclientv2.NewRoutingApi()

	currentQueue, _, getErr := routingAPI.GetRoutingQueue(d.Id())
	if getErr != nil {
		return diag.Errorf("Failed to read user %s: %s", d.Id(), getErr)
	}

	d.Set("name", *currentQueue.Name)
	d.Set("division_id", *currentQueue.Division.Id)

	if currentQueue.Description != nil {
		d.Set("description", *currentQueue.Description)
	}

	if currentQueue.AcwSettings != nil {
		if currentQueue.AcwSettings.WrapupPrompt != nil {
			d.Set("acw_wrapup_prompt", *currentQueue.AcwSettings.WrapupPrompt)
		}
		if currentQueue.AcwSettings.TimeoutMs != nil {
			d.Set("acw_timeout_ms", int(*currentQueue.AcwSettings.TimeoutMs))
		} else {
			// Computed value needs to be cleared if not set by the server
			d.Set("acw_timeout_ms", nil)
		}
	}

	if currentQueue.SkillEvaluationMethod != nil {
		d.Set("skill_evaluation_method", *currentQueue.SkillEvaluationMethod)
	}

	if currentQueue.MediaSettings != nil {
		if callSettings, ok := (*currentQueue.MediaSettings)[mediaSettingsKeyCall]; ok {
			d.Set("media_settings_call", []interface{}{flattenMediaSetting(callSettings)})
		}
		if callbackSettings, ok := (*currentQueue.MediaSettings)[mediaSettingsKeyCallback]; ok {
			d.Set("media_settings_callback", []interface{}{flattenMediaSetting(callbackSettings)})
		}
		if chatSettings, ok := (*currentQueue.MediaSettings)[mediaSettingsKeyChat]; ok {
			d.Set("media_settings_chat", []interface{}{flattenMediaSetting(chatSettings)})
		}
		if emailSettings, ok := (*currentQueue.MediaSettings)[mediaSettingsKeyEmail]; ok {
			d.Set("media_settings_email", []interface{}{flattenMediaSetting(emailSettings)})
		}
		if messageSettings, ok := (*currentQueue.MediaSettings)[mediaSettingsKeyMessage]; ok {
			d.Set("media_settings_message", []interface{}{flattenMediaSetting(messageSettings)})
		}
		if socialSettings, ok := (*currentQueue.MediaSettings)[mediaSettingsKeySocial]; ok {
			d.Set("media_settings_social", []interface{}{flattenMediaSetting(socialSettings)})
		}
		if videoSettings, ok := (*currentQueue.MediaSettings)[mediaSettingsKeyVideo]; ok {
			d.Set("media_settings_video", []interface{}{flattenMediaSetting(videoSettings)})
		}
	}

	if currentQueue.RoutingRules != nil {
		d.Set("routing_rules", flattenRoutingRules(*currentQueue.RoutingRules))
	}

	if currentQueue.Bullseye != nil && currentQueue.Bullseye.Rings != nil {
		d.Set("bullseye_rings", flattenBullseyeRings(*currentQueue.Bullseye.Rings))
	}

	if currentQueue.QueueFlow != nil && currentQueue.QueueFlow.Id != nil {
		d.Set("queue_flow_id", *currentQueue.QueueFlow.Id)
	}

	if currentQueue.WhisperPrompt != nil && currentQueue.WhisperPrompt.Id != nil {
		d.Set("whisper_prompt_id", *currentQueue.WhisperPrompt.Id)
	}

	if currentQueue.AutoAnswerOnly != nil {
		d.Set("auto_answer_only", *currentQueue.AutoAnswerOnly)
	}

	if currentQueue.EnableTranscription != nil {
		d.Set("enable_transcription", *currentQueue.EnableTranscription)
	}

	if currentQueue.EnableManualAssignment != nil {
		d.Set("enable_manual_assignment", *currentQueue.EnableManualAssignment)
	}

	if currentQueue.CallingPartyName != nil {
		d.Set("calling_party_name", *currentQueue.CallingPartyName)
	}

	if currentQueue.CallingPartyNumber != nil {
		d.Set("calling_party_number", *currentQueue.CallingPartyNumber)
	}

	if currentQueue.DefaultScripts != nil {
		d.Set("default_script_ids", flattenDefaultScripts(*currentQueue.DefaultScripts))
	}

	if currentQueue.OutboundMessagingAddresses != nil && currentQueue.OutboundMessagingAddresses.SmsAddress != nil {
		d.Set("outbound_messaging_sms_address_id", *currentQueue.OutboundMessagingAddresses.SmsAddress.Id)
	}

	if currentQueue.OutboundEmailAddress != nil {
		d.Set("outbound_email_address", []interface{}{flattenQueueEmailAddress(*currentQueue.OutboundEmailAddress)})
	}

	members, err := flattenQueueMembers(d.Id(), routingAPI)
	if err != nil {
		return err
	}
	d.Set("members", members)

	return nil
}

func updateQueue(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	name := d.Get("name").(string)
	divisionID := d.Get("division_id").(string)
	description := d.Get("description").(string)
	skillEvaluationMethod := d.Get("skill_evaluation_method").(string)
	autoAnswerOnly := d.Get("auto_answer_only").(bool)
	enableTranscription := d.Get("enable_transcription").(bool)
	enableManualAssignment := d.Get("enable_manual_assignment").(bool)
	callingPartyName := d.Get("calling_party_name").(string)
	callingPartyNumber := d.Get("calling_party_number").(string)

	routingAPI := platformclientv2.NewRoutingApi()
	authAPI := platformclientv2.NewAuthorizationApi()

	_, _, err := routingAPI.PutRoutingQueue(d.Id(), platformclientv2.Queuerequest{
		Name:                       &name,
		Description:                &description,
		MediaSettings:              buildSdkMediaSettings(d),
		RoutingRules:               buildSdkRoutingRules(d),
		Bullseye:                   buildSdkBullseyeSettings(d),
		AcwSettings:                buildSdkAcwSettings(d),
		SkillEvaluationMethod:      &skillEvaluationMethod,
		QueueFlow:                  buildSdkDomainEntityRef(d, "queue_flow_id"),
		WhisperPrompt:              buildSdkDomainEntityRef(d, "whisper_prompt_id"),
		AutoAnswerOnly:             &autoAnswerOnly,
		CallingPartyName:           &callingPartyName,
		CallingPartyNumber:         &callingPartyNumber,
		DefaultScripts:             buildSdkDefaultScriptsMap(d),
		OutboundMessagingAddresses: buildSdkQueueMessagingAddresses(d),
		OutboundEmailAddress:       buildSdkQueueEmailAddress(d),
		EnableTranscription:        &enableTranscription,
		EnableManualAssignment:     &enableManualAssignment,
	})
	if err != nil {
		return diag.Errorf("Error updating queue %s: %s", name, err)
	}

	if d.HasChange("division_id") {
		if divisionID == "" {
			// Default to home division
			homeDivision, diagErr := getHomeDivisionID()
			if diagErr != nil {
				return diagErr
			}
			divisionID = homeDivision
		}
		log.Printf("Updating division for queue %s to %s", name, divisionID)
		_, divErr := authAPI.PostAuthorizationDivisionObject(divisionID, "QUEUE", []string{d.Id()})
		if divErr != nil {
			return diag.Errorf("Failed to update division for queue %s: %s", name, divErr)
		}
	}

	diagErr := updateQueueMembers(d)
	if diagErr != nil {
		return diagErr
	}
	return readQueue(ctx, d, meta)
}

func deleteQueue(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	name := d.Get("name").(string)

	routingAPI := platformclientv2.NewRoutingApi()

	log.Printf("Deleting queue %s", name)
	_, err := routingAPI.DeleteRoutingQueue(d.Id(), true)
	if err != nil {
		return diag.Errorf("Failed to delete queue %s: %s", name, err)
	}

	// Queue deletes are not immediate. Query until queue is no longer found
	return diag.FromErr(resource.RetryContext(ctx, d.Timeout(schema.TimeoutDelete), func() *resource.RetryError {
		_, resp, err := routingAPI.GetRoutingQueue(d.Id())
		if err != nil {
			if resp != nil && resp.StatusCode == 404 {
				// Queue deleted
				return nil
			}
			return resource.NonRetryableError(fmt.Errorf("Error deleting queue %s: %s", d.Id(), err))
		}
		return resource.RetryableError(fmt.Errorf("Queue %s still exists", d.Id()))
	}))
}

func buildSdkMediaSettings(d *schema.ResourceData) *map[string]platformclientv2.Mediasetting {
	settings := make(map[string]platformclientv2.Mediasetting)

	mediaSettingsCall := d.Get("media_settings_call").([]interface{})
	if mediaSettingsCall != nil && len(mediaSettingsCall) > 0 {
		settings[mediaSettingsKeyCall] = buildSdkMediaSetting(mediaSettingsCall)
	}

	mediaSettingsCallback := d.Get("media_settings_callback").([]interface{})
	if mediaSettingsCallback != nil && len(mediaSettingsCallback) > 0 {
		settings[mediaSettingsKeyCallback] = buildSdkMediaSetting(mediaSettingsCallback)
	}

	mediaSettingsChat := d.Get("media_settings_chat").([]interface{})
	if mediaSettingsChat != nil && len(mediaSettingsChat) > 0 {
		settings[mediaSettingsKeyChat] = buildSdkMediaSetting(mediaSettingsChat)
	}

	mediaSettingsEmail := d.Get("media_settings_email").([]interface{})
	if mediaSettingsEmail != nil && len(mediaSettingsEmail) > 0 {
		settings[mediaSettingsKeyEmail] = buildSdkMediaSetting(mediaSettingsEmail)
	}

	mediaSettingsMessage := d.Get("media_settings_message").([]interface{})
	if mediaSettingsMessage != nil && len(mediaSettingsMessage) > 0 {
		settings[mediaSettingsKeyMessage] = buildSdkMediaSetting(mediaSettingsMessage)
	}

	mediaSettingsSocial := d.Get("media_settings_social").([]interface{})
	if mediaSettingsSocial != nil && len(mediaSettingsSocial) > 0 {
		settings[mediaSettingsKeySocial] = buildSdkMediaSetting(mediaSettingsSocial)
	}

	mediaSettingsVideo := d.Get("media_settings_video").([]interface{})
	if mediaSettingsVideo != nil && len(mediaSettingsVideo) > 0 {
		settings[mediaSettingsKeyVideo] = buildSdkMediaSetting(mediaSettingsVideo)
	}

	return &settings
}

func buildSdkMediaSetting(settings []interface{}) platformclientv2.Mediasetting {
	settingsMap := settings[0].(map[string]interface{})

	alertingTimeout := settingsMap["alerting_timeout_sec"].(int)
	serviceLevelPct := settingsMap["service_level_percentage"].(float64)
	serviceLevelDur := settingsMap["service_level_duration_ms"].(int)

	return platformclientv2.Mediasetting{
		AlertingTimeoutSeconds: &alertingTimeout,
		ServiceLevel: &platformclientv2.Servicelevel{
			Percentage: &serviceLevelPct,
			DurationMs: &serviceLevelDur,
		},
	}
}

func flattenMediaSetting(settings platformclientv2.Mediasetting) map[string]interface{} {
	settingsMap := make(map[string]interface{})
	settingsMap["alerting_timeout_sec"] = *settings.AlertingTimeoutSeconds
	settingsMap["service_level_percentage"] = *settings.ServiceLevel.Percentage
	settingsMap["service_level_duration_ms"] = *settings.ServiceLevel.DurationMs
	return settingsMap
}

func buildSdkRoutingRules(d *schema.ResourceData) *[]platformclientv2.Routingrule {
	var routingRules []platformclientv2.Routingrule
	if configRoutingRules, ok := d.GetOk("routing_rules"); ok {
		for _, configRule := range configRoutingRules.([]interface{}) {
			ruleSettings := configRule.(map[string]interface{})
			var sdkRule platformclientv2.Routingrule
			if operator, ok := ruleSettings["operator"].(string); ok {
				sdkRule.Operator = &operator
			}
			if threshold, ok := ruleSettings["threshold"]; ok {
				v := threshold.(int)
				sdkRule.Threshold = &v
			}
			if waitSeconds, ok := ruleSettings["wait_seconds"].(float64); ok {
				sdkRule.WaitSeconds = &waitSeconds
			}
			routingRules = append(routingRules, sdkRule)
		}
	}
	return &routingRules
}

func flattenRoutingRules(sdkRoutingRules []platformclientv2.Routingrule) []interface{} {
	rules := make([]interface{}, len(sdkRoutingRules))
	for i, sdkRule := range sdkRoutingRules {
		ruleSettings := make(map[string]interface{})
		if sdkRule.Operator != nil {
			ruleSettings["operator"] = *sdkRule.Operator
		}
		if sdkRule.Threshold != nil {
			ruleSettings["threshold"] = *sdkRule.Threshold
		}
		if sdkRule.WaitSeconds != nil {
			ruleSettings["wait_seconds"] = *sdkRule.WaitSeconds
		}
		rules[i] = ruleSettings
	}
	return rules
}

func buildSdkBullseyeSettings(d *schema.ResourceData) *platformclientv2.Bullseye {
	if configRings, ok := d.GetOk("bullseye_rings"); ok {
		var sdkRings []platformclientv2.Ring
		for _, configRing := range configRings.([]interface{}) {
			ringSettings := configRing.(map[string]interface{})
			var sdkRing platformclientv2.Ring
			if waitSeconds, ok := ringSettings["expansion_timeout_seconds"].(float64); ok {
				sdkRing.ExpansionCriteria = &[]platformclientv2.Expansioncriterium{
					{
						VarType:   &bullseyeExpansionTypeTimeout,
						Threshold: &waitSeconds,
					},
				}
			}
			if skillsToRemove, ok := ringSettings["skills_to_remove"]; ok {
				skillIds := skillsToRemove.(*schema.Set).List()
				if len(skillIds) > 0 {
					sdkSkillsToRemove := make([]platformclientv2.Skillstoremove, len(skillIds))
					for i, id := range skillIds {
						skillID := id.(string)
						sdkSkillsToRemove[i] = platformclientv2.Skillstoremove{
							Id: &skillID,
						}
					}
					sdkRing.Actions = &platformclientv2.Actions{
						SkillsToRemove: &sdkSkillsToRemove,
					}
				}
			}
			sdkRings = append(sdkRings, sdkRing)
		}
		return &platformclientv2.Bullseye{Rings: &sdkRings}
	}
	return nil
}

func flattenBullseyeRings(sdkRings []platformclientv2.Ring) []interface{} {
	rings := make([]interface{}, len(sdkRings))
	for i, sdkRing := range sdkRings {
		ringSettings := make(map[string]interface{})
		if sdkRing.ExpansionCriteria != nil {
			for _, criteria := range *sdkRing.ExpansionCriteria {
				if *criteria.VarType == bullseyeExpansionTypeTimeout {
					ringSettings["expansion_timeout_seconds"] = *criteria.Threshold
					break
				}
			}
		}
		if sdkRing.Actions != nil && sdkRing.Actions.SkillsToRemove != nil {
			skillIds := make([]interface{}, len(*sdkRing.Actions.SkillsToRemove))
			for s, skill := range *sdkRing.Actions.SkillsToRemove {
				skillIds[s] = *skill.Id
			}
			ringSettings["skills_to_remove"] = schema.NewSet(schema.HashString, skillIds)
		}
		rings[i] = ringSettings
	}
	return rings
}

func buildSdkAcwSettings(d *schema.ResourceData) *platformclientv2.Acwsettings {
	acwWrapupPrompt := d.Get("acw_wrapup_prompt").(string)

	acwSettings := platformclientv2.Acwsettings{
		WrapupPrompt: &acwWrapupPrompt, // Set or default
	}

	// Only set timeout for certain wrapup prompt types
	if acwWrapupPrompt == "MANDATORY_TIMEOUT" || acwWrapupPrompt == "MANDATORY_FORCED_TIMEOUT" || acwWrapupPrompt == "AGENT_REQUESTED" {
		acwTimeoutMs, hasTimeout := d.GetOk("acw_timeout_ms")
		if hasTimeout {
			timeout := acwTimeoutMs.(int)
			acwSettings.TimeoutMs = &timeout
		}
	}
	return &acwSettings
}

func buildSdkDefaultScriptsMap(d *schema.ResourceData) *map[string]platformclientv2.Script {
	if scriptIds, ok := d.GetOk("default_script_ids"); ok {
		scriptMap := scriptIds.(map[string]interface{})

		results := make(map[string]platformclientv2.Script)
		for k, v := range scriptMap {
			scriptID := v.(string)
			results[k] = platformclientv2.Script{Id: &scriptID}
		}
		return &results
	}
	return nil
}

func flattenDefaultScripts(sdkScripts map[string]platformclientv2.Script) map[string]interface{} {
	if len(sdkScripts) == 0 {
		return nil
	}

	results := make(map[string]interface{})
	for k, v := range sdkScripts {
		results[k] = *v.Id
	}
	return results
}

func validateMapCommTypes(val interface{}, _ cty.Path) diag.Diagnostics {
	if val == nil {
		return nil
	}

	commTypes := []string{"CALL", "CALLBACK", "CHAT", "COBROWSE", "EMAIL", "MESSAGE", "SOCIAL_EXPRESSION", "VIDEO", "SCREENSHARE"}
	m := val.(map[string]interface{})
	for k := range m {
		if !stringInSlice(k, commTypes) {
			return diag.Errorf("%s is an invalid communication type key.", k)
		}
	}
	return nil
}

func buildSdkQueueMessagingAddresses(d *schema.ResourceData) *platformclientv2.Queuemessagingaddresses {
	if _, ok := d.GetOk("outbound_messaging_sms_address_id"); ok {
		return &platformclientv2.Queuemessagingaddresses{
			SmsAddress: buildSdkDomainEntityRef(d, "outbound_messaging_sms_address_id"),
		}
	}
	return nil
}

func buildSdkQueueEmailAddress(d *schema.ResourceData) *platformclientv2.Queueemailaddress {
	outboundEmailAddress := d.Get("outbound_email_address").([]interface{})
	if outboundEmailAddress != nil && len(outboundEmailAddress) > 0 {
		settingsMap := outboundEmailAddress[0].(map[string]interface{})

		domainID := settingsMap["domain_id"].(string)
		routeID := settingsMap["route_id"].(string)

		return &platformclientv2.Queueemailaddress{
			Domain: &platformclientv2.Domainentityref{Id: &domainID},
			Route: &platformclientv2.Inboundroute{
				Id: &routeID,
			},
		}
	}
	return nil
}

func flattenQueueEmailAddress(settings platformclientv2.Queueemailaddress) map[string]interface{} {
	settingsMap := make(map[string]interface{})
	settingsMap["domain_id"] = *settings.Domain.Id
	settingsMap["route_id"] = *settings.Route.Id
	return settingsMap
}

func updateQueueMembers(d *schema.ResourceData) diag.Diagnostics {
	var newUserIds []string
	newUserRingNums := make(map[string]int)
	if members, ok := d.GetOk("members"); ok {
		memberList := members.(*schema.Set).List()
		newUserIds = make([]string, len(memberList))
		for i, member := range memberList {
			memberMap := member.(map[string]interface{})
			newUserIds[i] = memberMap["user_id"].(string)
			newUserRingNums[newUserIds[i]] = memberMap["ring_num"].(int)
		}
	} else {
		// Members not set in the config. Do not update.
		return nil
	}

	routingAPI := platformclientv2.NewRoutingApi()
	oldSdkUsers, err := getRoutingQueueMembers(d.Id(), routingAPI)
	if err != nil {
		return err
	}

	oldUserIds := make([]string, len(oldSdkUsers))
	oldUserRingNums := make(map[string]int)
	for i, user := range oldSdkUsers {
		oldUserIds[i] = *user.Id
		oldUserRingNums[oldUserIds[i]] = *user.RingNumber
	}

	if len(oldUserIds) > 0 {
		usersToRemove := sliceDifference(oldUserIds, newUserIds)
		err := updateMembersInChunks(d.Id(), usersToRemove, true, routingAPI)
		if err != nil {
			return err
		}
	}

	if len(newUserIds) > 0 {
		usersToAdd := sliceDifference(newUserIds, oldUserIds)
		err := updateMembersInChunks(d.Id(), usersToAdd, false, routingAPI)
		if err != nil {
			return err
		}
	}

	// Check for ring numbers to update
	for userID, newNum := range newUserRingNums {
		if oldNum, found := oldUserRingNums[userID]; found {
			if newNum != oldNum {
				// Number changed. Update ring number
				err := updateQueueUserRingNum(d.Id(), userID, newNum, routingAPI)
				if err != nil {
					return err
				}
			}
		} else if newNum != 1 {
			// New queue member. Update ring num if not set to the default of 1
			err := updateQueueUserRingNum(d.Id(), userID, newNum, routingAPI)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func updateMembersInChunks(queueID string, membersToUpdate []string, remove bool, api *platformclientv2.RoutingApi) diag.Diagnostics {
	// API restricts member adds/removes to 100 per call
	const maxBatchSize = 100
	for i := 0; i < len(membersToUpdate); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(membersToUpdate) {
			end = len(membersToUpdate)
		}
		var updateChunk []platformclientv2.Writableentity
		for _, id := range membersToUpdate[i:end] {
			updateChunk = append(updateChunk, platformclientv2.Writableentity{Id: &id})
		}

		if len(updateChunk) > 0 {
			// TODO: Ignoring err for now as SDK has a bug deserializing empty bodies
			/*_, _, err := */
			api.PostRoutingQueueMembers(queueID, updateChunk, remove)
			//if err != nil {
			//	return diag.Errorf("Failed to update members in queue %s: %s", queueID, err)
			//}
		}
	}
	return nil
}

func updateQueueUserRingNum(queueID string, userID string, ringNum int, api *platformclientv2.RoutingApi) diag.Diagnostics {
	_, _, err := api.PatchRoutingQueueMember(queueID, userID, platformclientv2.Queuemember{
		Id:         &userID,
		RingNumber: &ringNum,
	})
	if err != nil {
		return diag.Errorf("Failed to update ring number for queue %s user %s: %s", queueID, userID, err)
	}
	return nil
}

func getRoutingQueueMembers(queueID string, api *platformclientv2.RoutingApi) ([]platformclientv2.Queuemember, diag.Diagnostics) {
	const maxPageSize = 100

	var members []platformclientv2.Queuemember
	for pageNum := 1; ; pageNum++ {
		users, _, err := sdkGetRoutingQueueMembers(queueID, pageNum, maxPageSize, api)
		if err != nil {
			return nil, diag.Errorf("Failed to query users for queue %s: %s", queueID, err)
		}
		if users == nil || users.Entities == nil || len(*users.Entities) == 0 {
			return members, nil
		}
		for _, user := range *users.Entities {
			members = append(members, user)
		}
	}
}

func sdkGetRoutingQueueMembers(queueID string, pageNumber int, pageSize int, api *platformclientv2.RoutingApi) (*platformclientv2.Queuememberentitylisting, *platformclientv2.APIResponse, error) {
	// SDK does not support nil values for boolean query params yet, so we must manually construct the HTTP request for now
	apiClient := api.Configuration.APIClient

	// create path and map variables
	path := api.Configuration.BasePath + "/api/v2/routing/queues/{queueId}/members"
	path = strings.Replace(path, "{queueId}", fmt.Sprintf("%v", queueID), -1)

	headerParams := make(map[string]string)
	queryParams := make(map[string]string)
	formParams := url.Values{}
	var postBody interface{}
	var postFileName string
	var fileBytes []byte

	// oauth required
	if api.Configuration.AccessToken != "" {
		headerParams["Authorization"] = "Bearer " + api.Configuration.AccessToken
	}
	// add default headers if any
	for key := range api.Configuration.DefaultHeader {
		headerParams[key] = api.Configuration.DefaultHeader[key]
	}

	queryParams["pageSize"] = apiClient.ParameterToString(pageSize, "")
	queryParams["pageNumber"] = apiClient.ParameterToString(pageNumber, "")

	headerParams["Content-Type"] = "application/json"
	headerParams["Accept"] = "application/json"

	var successPayload *platformclientv2.Queuememberentitylisting
	response, err := apiClient.CallAPI(path, "GET", postBody, headerParams, queryParams, formParams, postFileName, fileBytes)
	if err != nil {
		// Nothing special to do here, but do avoid processing the response
	} else if err == nil && response.Error != nil {
		err = fmt.Errorf(response.ErrorMessage)
	} else {
		err = json.Unmarshal([]byte(response.RawBody), &successPayload)
	}
	return successPayload, response, err
}

func flattenQueueMembers(queueID string, api *platformclientv2.RoutingApi) (*schema.Set, diag.Diagnostics) {
	members, err := getRoutingQueueMembers(queueID, api)
	if err != nil {
		return nil, err
	}

	memberSet := schema.NewSet(schema.HashResource(queueMemberResource), []interface{}{})
	for _, member := range members {
		memberMap := make(map[string]interface{})
		memberMap["user_id"] = *member.Id
		memberMap["ring_num"] = *member.RingNumber
		memberSet.Add(memberMap)
	}

	return memberSet, nil
}