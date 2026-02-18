#!/bin/bash

# Pattern IDs
ATOMIC_DESIGN="00c4323b-9747-4344-9429-72249b3fd577"
DAISYUI="6deadb6c-cffc-4d1e-b4b9-3307a9d97ca8"
LOADING_STATES="a36f47d7-5acd-4556-97d1-b86564b54310"

# All component IDs from the change
COMPONENTS=(
  "05e57497-d689-430e-a297-2130f2ab1446"  # ThemeToggle
  "6654d384-442d-477e-b4d1-4973f0682a89"  # ThemePicker
  "65c8f1f0-f7d9-47ff-8d37-57eb9575a544"  # PageTitleHero
  "e356afcb-ee88-4517-82dd-4b07e943ebae"  # IconBadge
  "f7f5b09f-12cc-4e6d-ba41-bb369057613d"  # SidebarProjectItem
  "909db98e-85ce-460e-bed1-a13314f5371f"  # TableEmptyState
  "1939885b-fa7a-410b-a4a6-9336fa25f660"  # TableAvatarCell
  "6fe3a0db-3361-42e4-9b7b-2a6baa9da972"  # AvatarGroup
  "c6a1e39f-80e5-4542-99f2-49bac8506884"  # ChatPromptComposer
  "911b06f8-b1cf-4178-a367-65b0ff5d972e"  # ChatPromptActions
  "2bb385f2-f5f0-4e00-8e6a-8788b5a0ba44"  # ChatCtaCard
  "10035041-8f94-4171-a317-407503509506"  # NewChatCtas
  "c6f53809-5d50-401f-8611-b54dcbf8db7d"  # NotificationBell
  "f486419e-aeec-4786-b648-0e309b22d80f"  # NotificationRow
  "b7c9f6db-e7bb-4cb8-aa38-ea27ce2c88a5"  # NotificationTabButton
  "ed982b3c-19bc-4bba-86ba-c56d3e5f0800"  # TaskRow
  "ccfeb1b7-f874-4176-b8cb-2b931c50fcd7"  # TaskActionsPanel
  "37fad2be-0c1a-4e92-aced-eb0b4eed52fc"  # PendingInvitationCard
  "fda1ff9c-44c9-47ca-b8d3-01a91cae9a3f"  # ObjectRefCard
  "e2fadf4e-5e97-41e2-8854-c5a36253f1a9"  # ObjectRefLink
  "79ab75eb-1822-4dc0-8d2e-a23ac155b99c"  # ExtractionJobStatusBadge
  "5edc0ad8-c706-459e-8ea1-2b226a3da357"  # SystemStatusDropdown
  "8be7e516-5c8b-47b9-b5e9-e8b82b4b45b8"  # DebugInfoPanel
  "9265488a-eabf-4ec8-97e1-fdf302fc100e"  # SidebarSection
  "55ae829c-6325-4183-b7ec-ee86b91a5b2d"  # SidebarProjectDropdown
  "53ceaad0-8385-478e-8636-6afe67d25e11"  # Rightbar
  "6ee91c47-cd2b-470c-b966-a4fee8c121f4"  # Footer
  "24747038-5e2e-4e49-8679-3faada5e7017"  # RelationshipGraph
  "bcf26b62-0056-490c-99f1-9c7d834cf678"  # GraphViewerModal
  "1f11e729-e9ee-4ff8-b3c5-2317125e15e8"  # DeletionConfirmationModal
  "6f2cf00f-c0b6-44cd-9ecf-f0fa118f07e9"  # ExtractionConfigModal
  "2e34e65d-e249-4893-908a-400752501f71"  # ExtractionLogsModal
  "07413e3f-9da2-42ed-9ee3-57511068fa07"  # MergeComparisonModal
  "533102cd-b7ae-40ca-89f5-971f41f977cc"  # ExtractionJobCard
  "38bedf8c-72e4-4113-9a3f-7bcd4e4891da"  # ExtractionJobFilters
  "53002ef5-1643-4f96-819c-906b0ecd9872"  # JobDetailsView
  "3fb6018f-0a83-4ec3-aa53-ac6ff4e2074c"  # ChatObjectRefs
  "3fdbc1f7-27ba-429c-a7a5-3dc665b037a8"  # CodeDiffViewer
  "63727cf8-c855-4a8b-a7c8-8522450cd2f4"  # TasksInbox
  "c6723a31-ebe9-41e1-a6dc-91dd2a5d03b8"  # DiscoveryWizard
  "183ae020-c646-4c4c-ad6d-1fca2aefa056"  # KBPurposeEditor
  "023bbcea-3d47-4255-aa1b-9642bf64cb02"  # OrgAndProjectGate
  "b4d221b2-2941-4d5e-b72f-580de8bc7941"  # ProjectGate
  "4feae1ed-227a-49c5-a737-6c2ff7ec29b7"  # ViewAsBanner
)

echo "Applying atomic-design pattern to ${#COMPONENTS[@]} components..."
for comp in "${COMPONENTS[@]}"; do
  echo "  $comp"
done
