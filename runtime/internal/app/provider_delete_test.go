package app

import (
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/policy"
	"testing"
	"time"
)

func TestDeleteProviderCleansReferencesAndPreservesHistory(t *testing.T) {
	for _, disconnected := range []bool{false, true} {
		t.Run(map[bool]string{false: "active-exclusive-key", true: "disconnected-shared-key"}[disconnected], func(t *testing.T) {
			b, err := Build(t.Context(), testConfig(t))
			need(t, err)
			defer b.Close()
			p, _ := ownerSession(t, b)
			r, err := b.Services.Resources.SaveProvider(t.Context(), p, resource.SaveProviderRequest{Name: "Provider", Endpoint: "http://127.0.0.1:11434/v1/chat/completions", UseAPIKey: true, APIKey: core.NewSecret("delete-test-key"), Mutation: core.Mutation{IdempotencyKey: "provider"}})
			need(t, err)
			r, err = b.Services.Resources.ConfigureModel(t.Context(), p, resource.ConfigureModelRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "model", ExpectedVersion: r.Version}, Model: resource.ModelConfig{Name: "test", ContextLimit: 8192, SupportsTools: true}})
			need(t, err)
			connection, err := b.Services.Resources.ProviderConnection(t.Context(), p, r.ID)
			need(t, err)
			models, err := b.Services.Models.List(t.Context(), p)
			need(t, err)
			modelID := models[0].ID
			a, err := b.Services.Assistants.Hire(t.Context(), p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "assistant"}, Name: "Assistant", ModelID: modelID})
			need(t, err)
			_, err = b.Services.Conversations.SendUser(t.Context(), p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "message"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "Keep this conversation"}})
			need(t, err)
			_, err = b.Services.Grants.Grant(t.Context(), p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "grant"}, AssistantID: a.ID, ResourceID: r.ID, Actions: []policy.Action{policy.Read, policy.Execute}, Scope: policy.Scope{ModelID: modelID}})
			need(t, err)
			pref, err := b.Services.Settings.Get(t.Context(), p)
			need(t, err)
			pref.DefaultModelID = modelID
			_, err = b.Services.Settings.Update(t.Context(), p, settings.UpdateRequest{Mutation: core.Mutation{IdempotencyKey: "default", ExpectedVersion: pref.Version}, Settings: pref})
			need(t, err)
			var shared resource.Resource
			if disconnected {
				shared, err = b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "shared"}, Name: "Shared key", Kind: resource.Model, Provider: "openai-compatible", Binding: resource.Binding{Endpoint: connection.Endpoint, AllowedOrigins: []string{"http://127.0.0.1:11434"}, CredentialID: connection.CredentialID}})
				need(t, err)
				need(t, b.Services.Resources.Disconnect(t.Context(), p, resource.DisconnectRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "disconnect", ExpectedVersion: r.Version}}))
				r.Version++
			}
			req := resource.DeleteProviderRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "delete", ExpectedVersion: r.Version}}
			if !disconnected {
				need(t, audit.NewRepository(b.store).Append(t.Context(), audit.Record{ID: "delete-audit-collision", OwnerID: p.OwnerID, Action: "test", At: time.Now()}))
				ad := adapters{store: b.store}
				failing := resource.NewService(b.store, resource.Options{Authorizer: providerTestPolicy{}, Credentials: b.Services.Credentials, CheckVerification: b.Services.Auth.CheckVerification, OnDeleteProvider: ad.deleteProviderReferences, NewID: func() core.ID { return "delete-audit-collision" }})
				if err := failing.DeleteProvider(t.Context(), p, req); core.CodeOf(err) != core.Conflict {
					t.Fatalf("expected late audit failure: %v", err)
				}
				_, err = b.Services.Resources.ProviderConnection(t.Context(), p, r.ID)
				need(t, err)
				_, err = b.Services.Credentials.Get(t.Context(), p, connection.CredentialID)
				need(t, err)
				pref, err = b.Services.Settings.Get(t.Context(), p)
				need(t, err)
				if pref.DefaultModelID != modelID {
					t.Fatal("rollback lost default model")
				}
				grants, err := policy.NewRepository(b.store).Grants(t.Context(), p.OwnerID, policy.Query{ResourceID: r.ID})
				need(t, err)
				if grants.Items[0].RevokedAt != nil {
					t.Fatal("rollback revoked grant")
				}
			}
			need(t, b.Services.Resources.DeleteProvider(t.Context(), p, req))
			need(t, b.Services.Resources.DeleteProvider(t.Context(), p, req))
			_, err = b.Services.Resources.Get(t.Context(), p, r.ID)
			if core.CodeOf(err) != core.NotFound {
				t.Fatal("provider not deleted")
			}
			models, err = b.Services.Models.List(t.Context(), p)
			need(t, err)
			for _, model := range models {
				if model.ResourceID == r.ID {
					t.Fatal("deleted models remain available")
				}
			}
			pref, err = b.Services.Settings.Get(t.Context(), p)
			need(t, err)
			if pref.DefaultModelID != "" {
				t.Fatal("default model not cleared")
			}
			grants, err := policy.NewRepository(b.store).Grants(t.Context(), p.OwnerID, policy.Query{ResourceID: r.ID})
			need(t, err)
			if grants.Items[0].RevokedAt == nil {
				t.Fatal("grant not revoked")
			}
			messages, err := b.Services.Conversations.Messages(t.Context(), p, conversation.MessageQuery{ConversationID: a.ConversationID})
			need(t, err)
			if len(messages.Items) != 1 {
				t.Fatal("chat history was deleted")
			}
			if disconnected {
				_, err = b.Services.Credentials.Get(t.Context(), p, connection.CredentialID)
				need(t, err)
				need(t, b.Services.Resources.DeleteProvider(t.Context(), p, resource.DeleteProviderRequest{ID: shared.ID, Mutation: core.Mutation{IdempotencyKey: "delete-shared", ExpectedVersion: shared.Version}}))
			}
			_, err = b.Services.Credentials.Get(t.Context(), p, connection.CredentialID)
			if core.CodeOf(err) != core.NotFound {
				t.Fatal("unused credential not deleted")
			}
		})
	}
}
