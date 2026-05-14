package naming

import (
	"strings"
	"testing"

	"curio/internal/models"
)

func TestDefaultTemplatesValidateAndPreview(t *testing.T) {
	for _, template := range DefaultTemplates() {
		if err := Validate(template.TemplateType, template.Template); err != nil {
			t.Fatalf("%s should validate: %v", template.TemplateType, err)
		}
		preview, err := Preview(template.TemplateType, template.Template)
		if err != nil {
			t.Fatalf("%s should preview: %v", template.TemplateType, err)
		}
		if !strings.Contains(preview, ".mkv") {
			t.Fatalf("preview should keep extension: %s", preview)
		}
	}
}

func TestInvalidTemplateField(t *testing.T) {
	err := Validate(models.TemplateMovie, "movies/{show_title}.{extension}")
	if err == nil {
		t.Fatal("movie template should reject tv fields")
	}
}

func TestTechnicalFieldsValidate(t *testing.T) {
	err := Validate(models.TemplateMovie, "movies/{title} - {resolution} {video_codec} {hdr_format} {audio_codec} {audio_channels}.{extension}")
	if err != nil {
		t.Fatalf("technical fields should validate: %v", err)
	}
}

func TestRenderedPathCannotEscapeRoot(t *testing.T) {
	_, _, err := Render(models.TemplateMovie, "movies/{title}.{extension}", map[string]string{
		"title": "../escape", "extension": "mkv",
	}, "/data/Curio/staging")
	if err != nil {
		t.Fatalf("sanitized field should not escape root: %v", err)
	}
	if err := Validate(models.TemplateMovie, "../{title}.{extension}"); err == nil {
		t.Fatal("template with traversal should be rejected")
	}
}
