package provider

// Builtins returns the supported vendor definitions in display order
// (ascending Order). NewRegistry normalizes this into the runtime Registry.
func Builtins() []*ProviderDefinition {
	gen := func(u string) map[ModelType]string {
		return map[ModelType]string{ModelTypeGenerative: u}
	}
	both := func(g, e string) map[ModelType]string {
		return map[ModelType]string{ModelTypeGenerative: g, ModelTypeEmbedding: e}
	}
	emb := func(e string) map[ModelType]string {
		return map[ModelType]string{ModelTypeEmbedding: e}
	}
	keyFields := func() []CredentialField {
		return []CredentialField{
			{Name: "api_key", Required: true, Secret: true},
			{Name: "base_url", Required: false, Secret: false},
		}
	}

	defs := []*ProviderDefinition{
		// --- Generic OpenAI-compatible (no fixed endpoint) ---
		{
			Type:             ProviderGeneric,
			DisplayName:      "Generic OpenAI-compatible",
			Description:      "Generic OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("", ""),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true, ThinkingStyle: "chat_template_kwargs"},
			Order:            0,
			CredentialFields: keyFields(),
		},
		// --- WeKnora Cloud ---
		{
			Type:            ProviderWeKnoraCloud,
			DisplayName:     "WeKnora Cloud",
			Description:     "WeKnora Cloud via its OpenAI-compatible endpoint",
			Protocol:        ProtocolOpenAIChat,
			Auth:            AuthSigned,
			DefaultBaseURLs: both("https://weknora.weixin.qq.com", "https://weknora.weixin.qq.com"),
			ModelTypes:      []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy: CatalogOpenAIModels,
			Compat:          Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:           1,
			Names:           map[string]string{"zh-CN": "WeKnora 云"},
			CredentialFields: []CredentialField{
				{Name: "api_key", Required: true, Secret: true},
			},
		},
		// --- Aliyun DashScope ---
		{
			Type:             ProviderAliyun,
			DisplayName:      "Aliyun DashScope",
			Description:      "Aliyun DashScope via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            10,
			Names:            map[string]string{"zh-CN": "阿里云百炼"},
			CredentialFields: keyFields(),
		},
		// --- Zhipu GLM ---
		{
			Type:             ProviderZhipu,
			DisplayName:      "Zhipu GLM",
			Description:      "Zhipu GLM via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            11,
			Names:            map[string]string{"zh-CN": "智谱 GLM"},
			CredentialFields: keyFields(),
		},
		// --- Volcengine Ark (Doubao) ---
		{
			Type:             ProviderVolcengine,
			DisplayName:      "Volcengine Ark (Doubao)",
			Description:      "Volcengine Ark (Doubao) via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://ark.cn-beijing.volces.com/api/v3", "https://ark.cn-beijing.volces.com/api/v3/embeddings/multimodal"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_completion_tokens", SupportsToolChoice: true, ThinkingStyle: "thinking_type"},
			Order:            12,
			Names:            map[string]string{"zh-CN": "火山方舟"},
			CredentialFields: keyFields(),
		},
		// --- Tencent Hunyuan ---
		{
			Type:             ProviderHunyuan,
			DisplayName:      "Tencent Hunyuan",
			Description:      "Tencent Hunyuan via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://api.hunyuan.cloud.tencent.com/v1", "https://api.hunyuan.cloud.tencent.com/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            13,
			Names:            map[string]string{"zh-CN": "腾讯混元"},
			CredentialFields: keyFields(),
		},
		// --- SiliconFlow ---
		{
			Type:             ProviderSiliconFlow,
			DisplayName:      "SiliconFlow",
			Description:      "SiliconFlow via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://api.siliconflow.cn/v1", "https://api.siliconflow.cn/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            14,
			Names:            map[string]string{"zh-CN": "硅基流动"},
			CredentialFields: keyFields(),
		},
		// --- MiniMax ---
		{
			Type:             ProviderMiniMax,
			DisplayName:      "MiniMax",
			Description:      "MiniMax via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  gen("https://api.minimaxi.com/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            16,
			Names:            map[string]string{"zh-CN": "MiniMax"},
			CredentialFields: keyFields(),
		},
		// --- Moonshot ---
		{
			Type:             ProviderMoonshot,
			DisplayName:      "Moonshot",
			Description:      "Moonshot via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  gen("https://api.moonshot.ai/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            17,
			Names:            map[string]string{"zh-CN": "月之暗面"},
			CredentialFields: keyFields(),
		},
		// --- MiMo ---
		{
			Type:             ProviderMimo,
			DisplayName:      "MiMo",
			Description:      "MiMo via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  gen("https://api.xiaomimimo.com/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            18,
			CredentialFields: keyFields(),
		},
		// --- ModelScope ---
		{
			Type:             ProviderModelScope,
			DisplayName:      "ModelScope",
			Description:      "ModelScope via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://api-inference.modelscope.cn/v1", "https://api-inference.modelscope.cn/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            19,
			CredentialFields: keyFields(),
		},
		// --- Baidu Qianfan ---
		{
			Type:             ProviderQianfan,
			DisplayName:      "Baidu Qianfan",
			Description:      "Baidu Qianfan via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://qianfan.baidubce.com/v2", "https://qianfan.baidubce.com/v2"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            20,
			Names:            map[string]string{"zh-CN": "百度千帆"},
			CredentialFields: keyFields(),
		},
		// --- Qiniu ---
		{
			Type:             ProviderQiniu,
			DisplayName:      "Qiniu",
			Description:      "Qiniu via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://api.qnaigc.com/v1", "https://api.qnaigc.com/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            21,
			CredentialFields: keyFields(),
		},
		// --- LongCat ---
		{
			Type:             ProviderLongCat,
			DisplayName:      "LongCat",
			Description:      "LongCat via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  gen("https://api.longcat.chat/openai/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            22,
			CredentialFields: keyFields(),
		},
		// --- Tencent Cloud LKEAP ---
		{
			Type:             ProviderLKEAP,
			DisplayName:      "Tencent Cloud LKEAP",
			Description:      "Tencent Cloud LKEAP via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  gen("https://api.lkeap.cloud.tencent.com/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            23,
			Names:            map[string]string{"zh-CN": "腾讯云 LKEAP"},
			CredentialFields: keyFields(),
		},
		// --- OpenAI ---
		{
			Type:             ProviderOpenAI,
			DisplayName:      "OpenAI",
			Description:      "OpenAI API (GPT-4o, o1, etc.) authenticated via API key",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://api.openai.com/v1", "https://api.openai.com/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_completion_tokens", SupportsToolChoice: true},
			Order:            30,
			CredentialFields: keyFields(),
		},
		// --- DeepSeek ---
		{
			Type:             ProviderDeepSeek,
			DisplayName:      "DeepSeek",
			Description:      "DeepSeek AI models (deepseek-v4-flash, deepseek-v4-pro, deepseek-chat, deepseek-reasoner) via API key. Note: DeepSeek does not provide embeddings — configure a separate embedding provider.",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  gen("https://api.deepseek.com/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative},
			CatalogStrategy:  CatalogStatic,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: false, ThinkingStyle: "thinking"},
			Order:            30,
			CredentialFields: keyFields(),
		},
		// --- Azure OpenAI ---
		{
			Type:            ProviderAzureOpenAI,
			DisplayName:     "Azure OpenAI",
			Description:     "Azure OpenAI via its OpenAI-compatible endpoint",
			Protocol:        ProtocolOpenAIChat,
			Auth:            AuthAPIKeyHeader,
			DefaultBaseURLs: both("https://{resource}.openai.azure.com/openai/v1", "https://{resource}.openai.azure.com/openai/v1"),
			ModelTypes:      []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			ExtraFields: []ExtraField{
				{Key: "api_version", Label: "API version", Type: "string", Required: false, Placeholder: "2024-10-21"},
			},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            31,
			CredentialFields: keyFields(),
		},
		// --- Anthropic ---
		{
			Type:             ProviderAnthropic,
			DisplayName:      "Anthropic",
			Description:      "Anthropic Claude via the native Messages API",
			Protocol:         ProtocolAnthropicMessages,
			Auth:             AuthXAPIKey,
			DefaultBaseURLs:  gen("https://api.anthropic.com/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative},
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: false},
			Order:            32,
			CredentialFields: keyFields(),
		},
		// --- Google AI ---
		{
			Type:            ProviderGoogleAI,
			DisplayName:     "Google AI",
			Description:     "Google AI (Gemini API) authenticated via API key",
			Protocol:        ProtocolGoogleGenAI,
			Auth:            AuthGoogleAPIKey,
			DefaultBaseURLs: both("https://generativelanguage.googleapis.com/v1beta", "https://generativelanguage.googleapis.com/v1beta"),
			ModelTypes:      []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy: CatalogGoogleGenAI,
			Order:           33,
			CredentialFields: []CredentialField{
				{Name: "api_key", Description: "Google AI API key", Required: true, Secret: true},
			},
		},
		// --- Vertex AI ---
		{
			Type:            ProviderVertexAI,
			DisplayName:     "Vertex AI",
			Description:     "Google Cloud Vertex AI authenticated via service account",
			Protocol:        ProtocolGoogleGenAI,
			Auth:            AuthSigned,
			DefaultBaseURLs: both("https://{location}-aiplatform.googleapis.com", "https://{location}-aiplatform.googleapis.com"),
			ModelTypes:      []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy: CatalogGoogleGenAI,
			Order:           34,
			CredentialFields: []CredentialField{
				{Name: "service_account_json", Description: "GCP service account JSON key file contents", Required: true, Secret: true},
				{Name: "gcp_project", Description: "GCP project ID", Required: true, Secret: false},
				{Name: "location", Description: "GCP region (e.g. us-central1)", Required: true, Secret: false},
			},
		},
		// --- OpenRouter ---
		{
			Type:             ProviderOpenRouter,
			DisplayName:      "OpenRouter",
			Description:      "OpenRouter via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://openrouter.ai/api/v1", "https://openrouter.ai/api/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            40,
			CredentialFields: keyFields(),
		},
		// --- LiteLLM ---
		{
			Type:             ProviderLiteLLM,
			DisplayName:      "LiteLLM",
			Description:      "LiteLLM via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("", ""),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true, ThinkingStyle: "chat_template_kwargs"},
			Order:            41,
			CredentialFields: keyFields(),
		},
		// --- Requesty ---
		{
			Type:             ProviderRequesty,
			DisplayName:      "Requesty",
			Description:      "Requesty via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://router.requesty.ai/v1", "https://router.requesty.ai/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            42,
			CredentialFields: keyFields(),
		},
		// --- Jina AI ---
		{
			Type:             ProviderJina,
			DisplayName:      "Jina AI",
			Description:      "Jina AI via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  emb("https://api.jina.ai/v1"),
			ModelTypes:       []ModelType{ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            50,
			CredentialFields: keyFields(),
		},
		// --- NVIDIA NIM ---
		{
			Type:             ProviderNVIDIA,
			DisplayName:      "NVIDIA NIM",
			Description:      "NVIDIA NIM via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://integrate.api.nvidia.com/v1", "https://integrate.api.nvidia.com/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            51,
			CredentialFields: keyFields(),
		},
		// --- Novita ---
		{
			Type:             ProviderNovita,
			DisplayName:      "Novita",
			Description:      "Novita via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("https://api.novita.ai/openai/v1", "https://api.novita.ai/openai/v1"),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true},
			Order:            52,
			CredentialFields: keyFields(),
		},
		// --- GPUStack ---
		{
			Type:             ProviderGPUStack,
			DisplayName:      "GPUStack",
			Description:      "GPUStack via its OpenAI-compatible endpoint",
			Protocol:         ProtocolOpenAIChat,
			Auth:             AuthBearer,
			DefaultBaseURLs:  both("", ""),
			ModelTypes:       []ModelType{ModelTypeGenerative, ModelTypeEmbedding},
			CatalogStrategy:  CatalogOpenAIModels,
			Compat:           Compat{MaxTokensField: "max_tokens", SupportsToolChoice: true, ThinkingStyle: "chat_template_kwargs"},
			Order:            60,
			CredentialFields: keyFields(),
		},
	}

	for _, d := range defs {
		if d.Icon == nil {
			d.Icon = iconForVendor(d.Type)
		}
	}
	return defs
}

// builtinByType indexes the built-in vendor definitions by vendor ID, built once
// at package init so the catalog and credential paths can resolve a definition
// without holding a *Registry (nil-safe, lock-free).
var builtinByType = buildBuiltinIndex()

func buildBuiltinIndex() map[ProviderType]*ProviderDefinition {
	idx := make(map[ProviderType]*ProviderDefinition, len(Builtins()))
	for _, d := range Builtins() {
		idx[d.Type] = d
	}
	return idx
}

// definitionFor returns the built-in definition for a vendor ID, or nil when the
// vendor is not registered.
func definitionFor(pt ProviderType) *ProviderDefinition {
	return builtinByType[pt]
}

// hasEmbeddingModelType reports whether the definition serves an embedding
// model. Vendors with only generative model types have no embedding API.
func hasEmbeddingModelType(def *ProviderDefinition) bool {
	if def == nil {
		return false
	}
	for _, mt := range def.ModelTypes {
		if mt == ModelTypeEmbedding {
			return true
		}
	}
	return false
}
