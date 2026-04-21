package llm

// Message is a single chat message exchanged with the LLM.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest is the payload sent to the chat completions endpoint.
type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

// CompletionResponse is the parsed result of a chat completion call.
type CompletionResponse struct {
	Content      string
	PromptTokens int
	OutputTokens int
}
