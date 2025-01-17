package genesyscloud

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccDataSourceQualityFormsSurvey(t *testing.T) {
	var (
		formResource     = "quality-form"
		formDataResource = "quality-form-data"

		formName = "terraform-form-evaluations-" + uuid.NewString()
	)

	// Most basic survey form
	surveyForm1 := surveyFormStruct{
		name:     formName,
		language: "en-US",
		questionGroups: []surveyFormQuestionGroupStruct{
			{
				name: "Test Question Group 1",
				questions: []surveyFormQuestionStruct{
					{
						text:    "Was your problem solved?",
						varType: "multipleChoiceQuestion",
						answerOptions: []answerOptionStruct{
							{
								text:  "Yes",
								value: 1,
							},
							{
								text:  "No",
								value: 0,
							},
						},
					},
					{
						text:    "Multiple Choice Question.",
						varType: "multipleChoiceQuestion",
						answerOptions: []answerOptionStruct{
							{
								text:  "Option 1",
								value: 1,
							},
							{
								text:  "Option 2",
								value: 2,
							},
							{
								text:  "Option 3",
								value: 3,
							},
						},
					},
				},
			},
		},
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				Config: generateSurveyFormResource(
					formResource, &surveyForm1,
				) + generateQualityFormsSurveyDataSource(
					formDataResource,
					formName,
					"genesyscloud_quality_forms_survey."+formResource,
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.genesyscloud_quality_forms_survey."+formDataResource, "id", "genesyscloud_quality_forms_survey."+formResource, "id"),
				),
			},
		},
	})
}

func generateQualityFormsSurveyDataSource(
	resourceID string,
	name string,
	// Must explicitly use depends_on in terraform v0.13 when a data source references a resource
	// Fixed in v0.14 https://github.com/hashicorp/terraform/pull/26284
	dependsOnResource string) string {
	return fmt.Sprintf(`data "genesyscloud_quality_forms_survey" "%s" {
		name = "%s"
		depends_on=[%s]
	}
	`, resourceID, name, dependsOnResource)
}
