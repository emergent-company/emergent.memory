import json
import sys

def get_domain_key(key, obj_type):
    if obj_type == "UIComponent":
        if any(key.startswith(p) for p in ["ui-table", "ui-filter-", "ui-search-", "ui-search-bar", "ui-search-results", "ui-search-input"]):
            return "dom-search"
        if any(p in key for p in ["ui-meeting", "ui-agenda", "ui-attendee", "ui-protocol"]):
            return "dom-meeting"
        if any(p in key for p in ["ui-document", "ui-folder"]):
            return "dom-document"
        if any(p in key for p in ["ui-company", "ui-board"]):
            return "dom-company"
        if any(p in key for p in ["ui-user", "ui-profile"]):
            return "dom-user"
        if "ui-shareholder" in key:
            return "dom-shareholder"
        if "ui-onboarding" in key:
            return "dom-onboarding"
        if "ui-notification" in key:
            return "dom-notification"
        if any(p in key for p in ["ui-people", "ui-person"]):
            return "dom-people"
    
    elif obj_type == "Action":
        if any(p in key for p in ["act-meeting-", "act-meetings-", "act-attendee-", "act-meeting-candidate-"]):
            return "dom-meeting"
        if any(p in key for p in ["act-document-", "act-documents-", "act-folder-templates-", "act-document-history-model-", "act-document-options-model-", "act-document-upload-model-"]):
            return "dom-document"
        if any(p in key for p in ["act-company-", "act-companies-", "act-company-groups-"]):
            return "dom-company"
        if "act-user-" in key:
            return "dom-user"
        if "act-onboarding-" in key:
            return "dom-onboarding"
        if any(p in key for p in ["act-shareholder-", "act-shareholders-"]):
            return "dom-shareholder"
        if any(p in key for p in ["act-administration-", "act-system-admin-", "act-cms-model-", "act-data-migration-model-"]):
            return "dom-administration"
        if any(p in key for p in ["act-notification-", "act-notifications-", "act-message-"]):
            return "dom-notification"
        if "act-agenda-" in key:
            return "dom-agenda"
        if "act-ai-" in key:
            return "dom-ai"
        if "act-members-" in key:
            return "dom-people"

    elif obj_type == "APIEndpoint":
        if any(p in key for p in ["ep-meeting-", "ep-meeting-agenda-"]):
            return "dom-api-meeting"
        if any(p in key for p in ["ep-document-", "ep-document-sign-", "ep-folder-template-"]):
            return "dom-api-document"
        if any(p in key for p in ["ep-company-", "ep-company-request-"]):
            return "dom-api-company"
        if "ep-user-" in key:
            return "dom-api-user"
        if "ep-auth-" in key:
            return "dom-api-auth"
        if "ep-administration-" in key:
            return "dom-api-administration"
        if "ep-shareholder-" in key:
            return "dom-api-shareholder"
        if "ep-onboarding-" in key:
            return "dom-api-onboarding"
        if "ep-ai-" in key:
            return "dom-api-ai"

    elif obj_type == "Context":
        if "ctx-meeting-" in key:
            return "dom-meeting"
        if "ctx-document-" in key:
            return "dom-document"
        if any(p in key for p in ["ctx-company-", "ctx-companies-", "ctx-add-company-"]):
            return "dom-company"
        if any(p in key for p in ["ctx-user-", "ctx-account-"]):
            return "dom-user"
        if "ctx-onboarding-" in key:
            return "dom-onboarding"
        if "ctx-people-" in key:
            return "dom-people"
        if "ctx-administration-" in key:
            return "dom-administration"
        if "ctx-notifications-" in key:
            return "dom-notification"
        if "ctx-search-" in key:
            return "dom-search"
            
    return None

# Load domains
with open('domains.json', 'r') as f:
    domains_data = json.load(f).get("items", [])
domain_key_to_id = {d["key"]: d["id"] for d in domains_data if "key" in d}

types = ["UIComponent", "Action", "APIEndpoint", "Context"]
results = {}

for t in types:
    filename = t.lower() + "s.json"
    try:
        with open(filename, 'r') as f:
            raw_data = json.load(f)
            data = raw_data.get("items", [])
    except:
        data = []
    
    count = 0
    for obj in data:
        key = obj.get("key")
        obj_id = obj.get("id")
        if not key or not obj_id: continue
        domain_key = get_domain_key(key, t)
        if domain_key and domain_key in domain_key_to_id:
            domain_id = domain_key_to_id[domain_key]
            print(f"~/.memory/bin/memory graph relationships create --type belongs_to --from {obj_id} --to {domain_id} --project c64f599d-6d85-4fb5-b40a-6fcba437bc04 --server https://memory.emergent-company.ai --project-token emt_145ebccbaa71cb31877524d031eae22b4283e3470095635c46b75f399a6c5a43 --upsert")
            count += 1
    results[t] = count

# Print summary to stderr
for t, c in results.items():
    sys.stderr.write(f"{t}: {c}\n")
