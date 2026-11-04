package provider

import "embed"

// providerIcons embeds the vendor brand SVGs. They are MIT-licensed, sourced
// from Tencent/WeKnora's internal/models/providers/assets directory.
//
//go:embed assets/*.svg
var providerIcons embed.FS

// iconForVendor returns the embedded SVG bytes for a vendor id, mapping our ids
// to the WeKnora asset filenames: azure-openai → azure_openai.svg, google and
// google-vertex → gemini.svg, and every other id → <id>.svg. It returns nil when
// no icon exists for the vendor.
func iconForVendor(id ProviderType) []byte {
	name := string(id) + ".svg"
	switch id {
	case ProviderAzureOpenAI:
		name = "azure_openai.svg"
	case ProviderGoogleAI, ProviderVertexAI:
		name = "gemini.svg"
	}
	b, err := providerIcons.ReadFile("assets/" + name)
	if err != nil {
		return nil
	}
	return b
}
