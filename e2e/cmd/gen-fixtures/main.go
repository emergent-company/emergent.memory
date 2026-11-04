// gen-fixtures generates large test fixture files for e2e document tests.
//
// Usage:
//
//	go run ./cmd/gen-fixtures --out tests/cli/testdata
//
// Outputs:
//
//	meridian-full-report.txt  — ~500 KB plain-text financial report
//	meridian-full-report.pdf  — ~1 MB PDF version of the same report
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	out := flag.String("out", "tests/cli/testdata", "output directory")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}

	txt := buildFullReport()
	txtPath := filepath.Join(*out, "meridian-full-report.txt")
	if err := os.WriteFile(txtPath, []byte(txt), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write txt: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes)\n", txtPath, len(txt))

	pdf := buildPDF(txt)
	pdfPath := filepath.Join(*out, "meridian-full-report.pdf")
	if err := os.WriteFile(pdfPath, pdf, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write pdf: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes)\n", pdfPath, len(pdf))
}

// ─────────────────────────────────────────────────────────────────────────────
// Text report generator
// ─────────────────────────────────────────────────────────────────────────────

// Named entities woven throughout the report — deterministic, extractable.
var (
	partners = []string{
		"Robert Ashworth", "Priscilla Fontaine", "Kweku Mensah", "Yuki Tanaka",
		"Diana Ostrowski", "Alejandro Torres", "Sophie Leclercq", "Tomasz Kowalczyk",
		"Helena Voss", "Marcus Delacroix", "Ingrid Nystrom", "Basil Okonkwo",
		"Camille Renard", "Dmitri Volkov", "Fatima Al-Rashid", "James Whitfield",
	}
	companies = []string{
		"QuantumHealth", "NovaPay", "CarbonBridge", "SkyLogistics", "DataForge",
		"PulseGrid", "ArborTech", "MeridianAI", "OceanCore", "UrbanRoute",
		"BioNexus", "ClearWater Systems", "FractalRobotics", "SolarEdge Analytics",
		"HarbourLane Capital", "CoastalMed", "ApexSemi", "VaultChain", "Luminara",
		"TerraFund", "PinnacleOps", "ZephyrData", "IronBridge Infrastructure",
	}
	funds = []string{
		"Meridian Growth Fund IV", "Meridian Growth Fund V",
		"Meridian Infrastructure Fund II", "Meridian Infrastructure Fund III",
		"Meridian Climate Opportunities Fund I", "Meridian Climate Opportunities Fund II",
		"Meridian Credit Opportunities Fund I", "Meridian Special Situations Fund II",
	}
	decisions = []struct{ id, text string }{
		{"ADR-2024-001", "Approve continuation of NovaPay Series C investment at $45M"},
		{"ADR-2024-002", "Authorize CarbonBridge follow-on at reduced valuation with protective provisions"},
		{"ADR-2024-003", "Approve QuantumHealth acquisition of CoastalMed for $120M"},
		{"ADR-2024-004", "Authorize write-down of DataForge position by 40% to reflect market conditions"},
		{"ADR-2024-005", "Approve Meridian Growth Fund V first close at $800M"},
		{"ADR-2024-006", "Authorize SkyLogistics IPO readiness assessment and banker selection"},
		{"ADR-2024-007", "Approve MeridianAI Series A lead at $18M pre-money valuation"},
		{"ADR-2024-008", "Ratify ESG scoring framework update to align with SFDR Article 9"},
		{"ADR-2024-009", "Authorize IronBridge Infrastructure co-investment alongside Brookfield"},
		{"ADR-2024-010", "Approve ZephyrData bridge financing at 20% discount to Series B"},
		{"ADR-2025-001", "Approve VaultChain token economics restructuring and equity conversion"},
		{"ADR-2025-002", "Authorize Luminara secondary sale to HarbourLane Capital at 2.1x cost"},
		{"ADR-2025-003", "Approve FractalRobotics Series B participation at $60M valuation"},
		{"ADR-2025-004", "Ratify revised LP reporting standards effective Q1 2025"},
		{"ADR-2025-005", "Authorize TerraFund land acquisition programme Phase II"},
	}
)

func buildFullReport() string {
	var b strings.Builder

	// Header
	header(&b)

	// Part 1 — Executive Summary (repeated sections to add bulk)
	part(&b, "PART 1: EXECUTIVE SUMMARY")
	executiveSummary(&b)

	// Part 2 — Investment Committee & Governance
	part(&b, "PART 2: INVESTMENT COMMITTEE AND GOVERNANCE")
	committeeSection(&b)

	// Part 3 — Fund Performance (one section per fund × multiple years)
	part(&b, "PART 3: FUND-BY-FUND PERFORMANCE REVIEW")
	for i, fund := range funds {
		fundSection(&b, fund, i)
	}

	// Part 4 — Portfolio Company Deep Dives
	part(&b, "PART 4: PORTFOLIO COMPANY DEEP DIVES")
	for i, co := range companies {
		companySection(&b, co, i)
	}

	// Part 5 — Investment Committee Minutes (five years of quarterly meetings)
	part(&b, "PART 5: INVESTMENT COMMITTEE MEETING MINUTES")
	for year := 2021; year <= 2025; year++ {
		for q := 1; q <= 4; q++ {
			meetingMinutes(&b, year, q)
		}
	}

	// Part 6 — Formal Investment Decisions
	part(&b, "PART 6: FORMAL INVESTMENT DECISIONS REGISTER")
	decisionRegister(&b)

	// Part 7 — ESG & Impact
	part(&b, "PART 7: ESG AND IMPACT MEASUREMENT")
	esgSection(&b)

	// Part 8 — Risk Register
	part(&b, "PART 8: RISK REGISTER AND MITIGATION FRAMEWORK")
	riskSection(&b)

	// Part 9 — LP Relations & Capital Activity
	part(&b, "PART 9: LP RELATIONS AND CAPITAL ACTIVITY")
	lpSection(&b)

	// Part 10 — Outlook
	part(&b, "PART 10: STRATEGIC OUTLOOK 2025-2027")
	outlookSection(&b)

	// Part 11 — Board Meeting Summaries (one per portfolio company per year)
	part(&b, "PART 11: PORTFOLIO COMPANY BOARD MEETING SUMMARIES")
	for _, co := range companies {
		for year := 2022; year <= 2024; year++ {
			boardMeetingSummary(&b, co, year)
		}
	}

	// Part 12 — Quarterly LP Update Letters
	part(&b, "PART 12: QUARTERLY LP UPDATE LETTERS")
	for year := 2022; year <= 2024; year++ {
		for q := 1; q <= 4; q++ {
			lpUpdateLetter(&b, year, q)
		}
	}

	// Part 13 — Valuation Methodology and Audit Trail
	part(&b, "PART 13: VALUATION METHODOLOGY AND FAIR VALUE AUDIT TRAIL")
	valuationSection(&b)

	// Part 14 — Fund Legal and Compliance Review
	part(&b, "PART 14: LEGAL AND COMPLIANCE REVIEW")
	complianceSection(&b)

	// Appendix A — Bios
	part(&b, "APPENDIX A: INVESTMENT COMMITTEE MEMBER BIOGRAPHIES")
	biographies(&b)

	// Appendix B — Glossary (adds bulk)
	part(&b, "APPENDIX B: GLOSSARY OF TERMS")
	glossary(&b)

	// Appendix C — Fund Terms Summary
	part(&b, "APPENDIX C: FUND TERMS SUMMARY")
	fundTermsSummary(&b)

	// Appendix D — Co-investment Log
	part(&b, "APPENDIX D: CO-INVESTMENT LOG")
	coInvestmentLog(&b)

	return b.String()
}

func header(b *strings.Builder) {
	b.WriteString("MERIDIAN CAPITAL PARTNERS\n")
	b.WriteString("FULL PORTFOLIO AND GOVERNANCE REPORT\n")
	b.WriteString("Fiscal Years 2023–2025 | Prepared by: Investment Committee Secretariat\n")
	b.WriteString("Date: March 31, 2025\n")
	b.WriteString("STRICTLY CONFIDENTIAL — FOR AUTHORISED RECIPIENTS ONLY\n")
	b.WriteString(strings.Repeat("=", 78) + "\n\n")
}

func part(b *strings.Builder, title string) {
	b.WriteString("\n" + strings.Repeat("=", 78) + "\n")
	b.WriteString(title + "\n")
	b.WriteString(strings.Repeat("=", 78) + "\n\n")
}

func section(b *strings.Builder, title string) {
	b.WriteString("\n" + strings.Repeat("-", 60) + "\n")
	b.WriteString(title + "\n")
	b.WriteString(strings.Repeat("-", 60) + "\n\n")
}

func executiveSummary(b *strings.Builder) {
	paras := []string{
		"Meridian Capital Partners closed Fiscal Year 2024 with $6.8 billion in assets under management across eight active funds spanning growth equity, infrastructure, climate transition, credit opportunities, and special situations strategies.",
		"The firm's flagship Meridian Growth Fund IV delivered a net IRR of 19.2% through December 31, 2024, outperforming the Cambridge Associates Global Venture benchmark of 14.1% by 510 basis points.  Robert Ashworth, Managing Partner, attributed the outperformance to concentrated positions in high-conviction software and healthcare technology companies.",
		"Meridian Infrastructure Fund II delivered a net IRR of 11.8%, in line with the firm's underwriting assumptions.  Kweku Mensah, General Partner leading the infrastructure practice, noted that rising interest rates created headwinds in the second half of 2024 but that the portfolio's contracted cash flows provided resilience.",
		"Meridian Climate Opportunities Fund I achieved a net IRR of 14.7% in its third year, driven primarily by the unrealised appreciation in CarbonBridge and TerraFund.  Sophie Leclercq, Head of ESG and Impact Measurement, highlighted that the fund's carbon avoidance metrics exceeded the target by 23%.",
		"The Investment Committee, chaired by Robert Ashworth and comprising fifteen senior professionals, convened twelve times during fiscal 2024 and ratified fifteen formal investment decisions.  Full minutes are reproduced in Part 5 of this report.",
		"Alejandro Torres, CFO, reported that the firm's cost-to-income ratio improved to 48.3% from 51.7% in the prior year, reflecting operational leverage as AUM grew by $1.4 billion.  Fee-related earnings increased to $112M, representing an 18% increase year-on-year.",
		"Tomasz Kowalczyk, Chief Risk Officer, conducted a comprehensive portfolio risk review in Q4 2024.  The review identified three companies — DataForge, ZephyrData, and VaultChain — as requiring elevated monitoring due to balance-sheet stress and deteriorating unit economics.  Appropriate reserves and valuation adjustments have been recorded.",
		"Priscilla Fontaine, General Partner for Growth Equity, led the firm's largest single investment in fiscal 2024: the $45M participation in NovaPay's Series C round at a $220M pre-money valuation.  NovaPay's annualised payment volume reached $2.3B by year-end, a 340% increase from the prior year.",
		"Diana Ostrowski, Partner covering Consumer and Healthcare, oversaw QuantumHealth's acquisition of CoastalMed, creating a vertically integrated digital-health platform with over 1.2 million active patients.  The combined entity is on track for a 2026 IPO.",
		"Ingrid Nystrom joined the Investment Committee as Partner, Technology Sector, replacing Yuki Tanaka who was promoted to Managing Director with a focus on Asia-Pacific expansion.  Marcus Delacroix was promoted from Principal to Partner in the Infrastructure practice, effective January 2025.",
	}
	for _, p := range paras {
		b.WriteString(p + "\n\n")
	}

	section(b, "Key Performance Indicators — Fiscal Year 2024")
	kpis := [][2]string{
		{"Total AUM", "$6.8 billion"},
		{"Fee-Related Earnings", "$112 million"},
		{"Growth Fund IV Net IRR", "19.2%"},
		{"Infrastructure Fund II Net IRR", "11.8%"},
		{"Climate Fund I Net IRR", "14.7%"},
		{"Active Portfolio Companies", "47"},
		{"Investment Committee Meetings", "12"},
		{"Formal Decisions Ratified", "15"},
		{"New Investments (FY2024)", "8"},
		{"Follow-On Investments", "11"},
		{"Exits / Realisations", "4"},
		{"Capital Deployed", "$680 million"},
		{"Capital Returned to LPs", "$420 million"},
	}
	for _, kpi := range kpis {
		fmt.Fprintf(b, "  %-42s %s\n", kpi[0], kpi[1])
	}
	b.WriteString("\n")
}

func committeeSection(b *strings.Builder) {
	section(b, "Investment Committee Composition")
	members := []struct {
		name, role, focus string
	}{
		{"Robert Ashworth", "Managing Partner, Chair", "Strategy, Growth Equity, LP Relations"},
		{"Priscilla Fontaine", "General Partner", "Growth Equity, Software, FinTech"},
		{"Kweku Mensah", "General Partner", "Infrastructure, Energy Transition, Climate"},
		{"Yuki Tanaka", "Managing Director", "Technology, Asia-Pacific Expansion"},
		{"Diana Ostrowski", "Partner", "Consumer Technology, Healthcare"},
		{"Alejandro Torres", "CFO / Partner", "Finance, Operations, Compliance"},
		{"Sophie Leclercq", "Partner", "ESG, Impact Measurement, Sustainability"},
		{"Tomasz Kowalczyk", "Partner / CRO", "Risk Management, Portfolio Monitoring"},
		{"Helena Voss", "Partner", "Credit Opportunities, Special Situations"},
		{"Marcus Delacroix", "Partner", "Infrastructure, Renewables"},
		{"Ingrid Nystrom", "Partner", "Technology Sector, Deep Tech"},
		{"Basil Okonkwo", "Partner", "Africa & Emerging Markets"},
		{"Camille Renard", "Partner", "Consumer, Retail, Luxury"},
		{"Dmitri Volkov", "Partner", "Eastern Europe, Cybersecurity"},
		{"Fatima Al-Rashid", "Partner", "MENA, Islamic Finance"},
		{"James Whitfield", "General Counsel / Partner", "Legal, Regulatory, Fund Structuring"},
	}
	fmt.Fprintf(b, "  %-28s %-32s %s\n", "NAME", "ROLE", "PRIMARY FOCUS")
	b.WriteString("  " + strings.Repeat("-", 80) + "\n")
	for _, m := range members {
		fmt.Fprintf(b, "  %-28s %-32s %s\n", m.name, m.role, m.focus)
	}
	b.WriteString("\n")

	section(b, "Governance Framework")
	govParas := []string{
		"The Investment Committee operates under a formal governance framework adopted in 2019 and most recently updated in January 2024.  All investment decisions above $5M require a quorum of at least seven members, with approval by a simple majority of those present.",
		"Decisions above $50M require supermajority approval (ten of sixteen members) and a mandatory 48-hour deliberation period following the initial presentation.  Robert Ashworth, as Chair, holds a casting vote in the event of a tie.",
		"James Whitfield, General Counsel, maintains the decisions register and ensures that all resolutions are consistent with the constitutional documents of each fund.  Tomasz Kowalczyk provides a risk assessment memorandum for every investment above $10M.",
		"The Investment Committee adopted a revised conflicts-of-interest policy in March 2024, requiring disclosure of any direct or indirect financial interest held by a committee member in a target company or its competitors.  Helena Voss recused herself from the ZephyrData bridge financing discussion under this policy.",
		"ESG screening, led by Sophie Leclercq, is mandatory for all new investments.  The firm's responsible investment policy prohibits investments in companies deriving more than 15% of revenue from thermal coal, tobacco, or controversial weapons.",
	}
	for _, p := range govParas {
		b.WriteString(p + "\n\n")
	}
}

func fundSection(b *strings.Builder, fund string, idx int) {
	section(b, fund)
	vintage := 2018 + (idx / 2)
	targetSize := 500 + idx*200
	actualSize := targetSize - 50 + idx*30
	irr := 12.0 + float64(idx%5)*1.8
	moic := 1.4 + float64(idx%4)*0.3
	fmt.Fprintf(b, "Vintage Year:          %d\n", vintage)
	fmt.Fprintf(b, "Target Fund Size:      $%dM\n", targetSize)
	fmt.Fprintf(b, "Actual Commitments:    $%dM\n", actualSize)
	fmt.Fprintf(b, "Net IRR (to date):     %.1f%%\n", irr)
	fmt.Fprintf(b, "Net MOIC (to date):    %.2fx\n", moic)
	fmt.Fprintf(b, "DPI:                   %.2fx\n", moic*0.4)
	fmt.Fprintf(b, "RVPI:                  %.2fx\n", moic*0.6)
	b.WriteString("\n")

	// Assign a subset of partners as key team
	p1 := partners[idx%len(partners)]
	p2 := partners[(idx+1)%len(partners)]
	p3 := partners[(idx+3)%len(partners)]
	fmt.Fprintf(b, "Lead Partner:          %s\n", p1)
	fmt.Fprintf(b, "Co-Lead:               %s\n", p2)
	fmt.Fprintf(b, "Risk Officer:          %s\n\n", p3)

	// Assign a subset of companies
	b.WriteString("Portfolio Companies:\n")
	for j := 0; j < 4; j++ {
		co := companies[(idx*3+j)%len(companies)]
		cost := 10 + (idx+j)*8
		fair := cost + cost/3 + j*5
		fmt.Fprintf(b, "  %-28s Cost: $%dM   Fair Value: $%dM\n", co, cost, fair)
	}
	b.WriteString("\n")

	narratives := []string{
		fmt.Sprintf("%s has performed in line with the fund's investment thesis. %s led the investment process and continues to serve on the portfolio company's board.  The position was last valued at %dx cost.", companies[idx%len(companies)], p1, 2+idx%3),
		fmt.Sprintf("The fund completed %d new investments and %d follow-on investments during fiscal 2024.  Total capital deployed since inception stands at $%dM.", 2+idx%3, 3+idx%4, actualSize*60/100),
		fmt.Sprintf("%s chairs the fund's Advisory Committee, which met three times during 2024 to review performance and approve valuation methodologies.", p3),
		fmt.Sprintf("The fund's investment period %s.  The team is focused on value creation at the existing portfolio rather than new origination.", map[bool]string{true: "closed on December 31, 2024", false: "remains open until December 31, 2025"}[idx%2 == 0]),
	}
	for _, n := range narratives {
		b.WriteString(n + "\n\n")
	}
}

func companySection(b *strings.Builder, company string, idx int) {
	section(b, company)
	sector := []string{"FinTech", "HealthTech", "Climate / CleanTech", "Enterprise SaaS", "Infrastructure", "Consumer Tech", "Deep Tech / AI", "Logistics"}[idx%8]
	stage := []string{"Series B", "Series C", "Growth", "Pre-IPO", "Series A", "Series D"}[idx%6]
	cost := 15 + idx*12
	fair := cost + cost/2 + idx*7
	partner := partners[idx%len(partners)]
	board := partners[(idx+2)%len(partners)]

	fmt.Fprintf(b, "Sector:                %s\n", sector)
	fmt.Fprintf(b, "Stage at Entry:        %s\n", stage)
	fmt.Fprintf(b, "Cost Basis:            $%dM\n", cost)
	fmt.Fprintf(b, "Current Fair Value:    $%dM\n", fair)
	fmt.Fprintf(b, "Unrealised Gain/Loss:  $%dM\n", fair-cost)
	fmt.Fprintf(b, "Meridian Board Seat:   %s\n", partner)
	fmt.Fprintf(b, "Board Observer:        %s\n\n", board)

	paras := []string{
		fmt.Sprintf("%s was founded in %d and has grown to become a leading %s platform serving enterprise clients across North America and Europe.  The company employs %d full-time staff and operates from offices in London, New York, and Singapore.", company, 2016+idx%6, strings.ToLower(sector), 80+idx*40),
		fmt.Sprintf("Meridian first invested in %s at the %s round.  %s led the investment committee presentation and was appointed to the board following close.  The original investment thesis centred on the company's differentiated technology and a large, underpenetrated addressable market.", company, stage, partner),
		fmt.Sprintf("Revenue for fiscal 2024 grew %d%% year-on-year to $%dM ARR.  Gross margin expanded from %d%% to %d%% as the company scaled its infrastructure and reduced hosting costs through a cloud-cost optimisation programme.", 60+idx*10, 8+idx*6, 55+idx%15, 62+idx%15),
		fmt.Sprintf("The company raised a $%dM %s round in Q%d 2024, led by a prominent global technology investor, with Meridian participating pro-rata.  Post-round, Meridian's ownership stands at %.1f%% on a fully diluted basis.", cost*2, []string{"Series B", "Series C", "Series D", "Growth"}[idx%4], 1+idx%4, 12.0+float64(idx%8)*1.5),
		fmt.Sprintf("%s remains the company's primary strategic risk.  %s and the management team have implemented a comprehensive mitigation plan, including %s.  Tomasz Kowalczyk has reviewed and approved the risk mitigation framework.", []string{"Customer concentration", "Regulatory headwinds", "Competitive pressure", "Execution risk", "Market timing"}[idx%5], board, []string{"diversifying the customer base", "engaging proactively with regulators", "accelerating product differentiation", "strengthening the leadership team", "extending the cash runway to 36 months"}[idx%5]),
		fmt.Sprintf("The company's management team, led by CEO %s, has consistently delivered against its quarterly milestones.  The board met six times in fiscal 2024 and approved a revised three-year strategic plan targeting $%dM ARR by 2027.", []string{"Alexandra Brandt", "Chen Wei", "Nadia Petrov", "Samuel Osei", "Laura Bergmann", "Carlos Mendez", "Anya Sharma", "Patrick O'Brien"}[idx%8], (8+idx*6)*5),
	}
	for _, p := range paras {
		b.WriteString(p + "\n\n")
	}
}

func meetingMinutes(b *strings.Builder, year, quarter int) {
	months := []string{"", "January", "April", "July", "October"}
	section(b, fmt.Sprintf("Investment Committee Meeting — Q%d %d", quarter, year))
	date := fmt.Sprintf("%s %d, %d", months[quarter], 14+quarter*2, year)
	chair := "Robert Ashworth"
	secretary := "James Whitfield"
	present := partners[:10+quarter%4]
	apologies := partners[10+quarter%4 : 12+quarter%4]

	fmt.Fprintf(b, "Date:        %s\n", date)
	fmt.Fprintf(b, "Chair:       %s\n", chair)
	fmt.Fprintf(b, "Secretary:   %s\n", secretary)
	b.WriteString("Present:     ")
	b.WriteString(strings.Join(present, ", "))
	b.WriteString("\n")
	b.WriteString("Apologies:   ")
	b.WriteString(strings.Join(apologies, ", "))
	b.WriteString("\n\n")

	b.WriteString("1. OPENING AND QUORUM\n\n")
	fmt.Fprintf(b, "%s opened the meeting at 09:00 and confirmed that a quorum of %d members was present, satisfying the constitutional requirement for binding resolutions.\n\n", chair, len(present))

	b.WriteString("2. MINUTES OF PREVIOUS MEETING\n\n")
	prevMon := months[((quarter-2+4)%4)+1]
	prevYear := year
	if quarter == 1 {
		prevYear--
	}
	fmt.Fprintf(b, "The minutes of the meeting held in %s %d were reviewed and approved without amendment.  %s confirmed that all action items had been completed.\n\n", prevMon, prevYear, secretary)

	b.WriteString("3. PORTFOLIO REVIEW\n\n")
	for j := 0; j < 3; j++ {
		co := companies[(year*10+quarter*3+j)%len(companies)]
		partner := partners[(year+quarter+j)%len(partners)]
		fmt.Fprintf(b, "%s presented an update on %s.  Revenue for the trailing twelve months was $%dM, representing %d%% growth year-on-year.  %s confirmed that the company is tracking ahead of plan and remains on course for the milestones outlined in the most recent board paper.\n\n",
			partner, co, 10+j*15+quarter*5, 40+j*20+quarter*10, partner)
	}

	b.WriteString("4. NEW INVESTMENT PROPOSALS\n\n")
	numProps := 1 + quarter%3
	for j := 0; j < numProps; j++ {
		co := companies[(year*5+quarter*7+j*3)%len(companies)]
		amt := 10 + j*15 + quarter*8
		proposer := partners[(year+quarter*2+j)%len(partners)]
		fmt.Fprintf(b, "%s presented an investment memorandum for %s, proposing a $%dM commitment at a $%dM pre-money valuation.  The committee discussed the opportunity at length, focusing on the competitive landscape, management team quality, and exit pathway.  Following deliberation, the committee approved the investment subject to satisfactory completion of legal due diligence and reference checks.\n\n",
			proposer, co, amt, amt*5)
	}

	b.WriteString("5. RISK ITEMS\n\n")
	riskCo := companies[(year*3+quarter*4)%len(companies)]
	riskPartner := "Tomasz Kowalczyk"
	fmt.Fprintf(b, "%s presented the quarterly risk report.  %s was flagged as requiring elevated monitoring following a miss on Q%d revenue targets.  The committee instructed the lead partner to arrange a call with the CEO within ten business days and to report back at the next meeting.\n\n", riskPartner, riskCo, quarter)

	b.WriteString("6. ANY OTHER BUSINESS\n\n")
	fmt.Fprintf(b, "%s raised a housekeeping matter regarding LP reporting timelines.  It was agreed that the next LP update letter would be issued by the 15th of the following month.\n\n", secretary)

	b.WriteString("7. DATE OF NEXT MEETING\n\n")
	nextQ := quarter%4 + 1
	nextYear := year
	if nextQ == 1 {
		nextYear++
	}
	fmt.Fprintf(b, "The next meeting of the Investment Committee will be held in %s %d.  %s to circulate the agenda no fewer than five business days in advance.\n\n", months[nextQ], nextYear, secretary)

	b.WriteString("Meeting closed at 17:30.\n\n")
	fmt.Fprintf(b, "Signed: %s (Chair)\n", chair)
	fmt.Fprintf(b, "Countersigned: %s (Secretary)\n\n", secretary)
}

func decisionRegister(b *strings.Builder) {
	section(b, "Formal Investment Decisions — FY2024 and FY2025 YTD")
	fmt.Fprintf(b, "  %-18s %-12s %-12s  %s\n", "DECISION ID", "DATE", "VOTE", "DESCRIPTION")
	b.WriteString("  " + strings.Repeat("-", 90) + "\n")
	votes := []string{"16-0", "15-1", "14-2", "13-3", "12-4", "11-5", "10-6"}
	dates := []string{
		"Jan 14, 2024", "Feb 20, 2024", "Mar 18, 2024", "Apr 22, 2024", "May 13, 2024",
		"Jun 17, 2024", "Jul 15, 2024", "Aug 19, 2024", "Sep 16, 2024", "Oct 14, 2024",
		"Nov 18, 2024", "Dec 09, 2024", "Jan 20, 2025", "Feb 17, 2025", "Mar 10, 2025",
	}
	for i, d := range decisions {
		fmt.Fprintf(b, "  %-18s %-12s %-12s  %s\n", d.id, dates[i], votes[i%len(votes)], d.text)
	}
	b.WriteString("\n")

	section(b, "Decision Narratives")
	for i, d := range decisions {
		proposer := partners[i%len(partners)]
		seconder := partners[(i+3)%len(partners)]
		b.WriteString(d.id + " — " + d.text + "\n\n")
		fmt.Fprintf(b, "Proposed by: %s.  Seconded by: %s.\n\n", proposer, seconder)
		fmt.Fprintf(b, "Background: The Investment Committee reviewed a detailed memorandum prepared by %s covering the strategic rationale, financial projections, due diligence findings, and risk assessment for this resolution.  The committee noted the strong alignment with the fund's investment mandate and the attractive risk/return profile of the proposed action.\n\n", proposer)
		fmt.Fprintf(b, "Discussion: %s noted the potential downside scenario and requested that %s prepare a sensitivity analysis for distribution within 48 hours.  %s confirmed that all legal conditions precedent had been satisfied.\n\n", partners[(i+1)%len(partners)], partners[(i+2)%len(partners)], "James Whitfield")
		fmt.Fprintf(b, "Resolution: Approved by vote of %s.\n\n", votes[i%len(votes)])
	}
}

func esgSection(b *strings.Builder) {
	section(b, "ESG Framework and Policy")
	esgParas := []string{
		"Meridian Capital Partners is a signatory to the United Nations Principles for Responsible Investment (UNPRI) and the Net Zero Asset Managers initiative.  The firm's ESG policy, maintained by Sophie Leclercq, was last comprehensively updated in Q1 2024 to align with the EU Sustainable Finance Disclosure Regulation (SFDR) Article 9 requirements for the Climate Opportunities funds.",
		"All new investments are subject to a pre-investment ESG screening, which scores each target company across four dimensions: Environmental Impact, Social Responsibility, Governance Quality, and Stakeholder Alignment.  Companies scoring below 60 out of 100 on the composite index are subject to enhanced due diligence and may not be approved without a remediation plan.",
		"Sophie Leclercq chairs the ESG Committee, which comprises herself, Kweku Mensah, Diana Ostrowski, and an independent ESG advisor.  The committee meets quarterly and reviews portfolio-level ESG performance against the firm's impact thesis.",
		"The firm's carbon footprint measurement programme, launched in 2022, now covers 89% of the portfolio by fair value.  Aggregate Scope 1 and Scope 2 emissions across the portfolio declined by 12% in fiscal 2024, ahead of the 8% annual reduction target.",
		"Meridian's engagement policy requires lead partners to raise ESG-related matters at a minimum of two board meetings per year for each portfolio company.  Priscilla Fontaine reported that NovaPay achieved carbon neutrality for its Scope 1 and 2 emissions in Q3 2024, making it the first company in the growth equity portfolio to do so.",
	}
	for _, p := range esgParas {
		b.WriteString(p + "\n\n")
	}

	section(b, "ESG Scorecard — FY2024")
	fmt.Fprintf(b, "  %-28s %8s %8s %8s %8s %8s\n", "COMPANY", "ENV", "SOCIAL", "GOV", "ALIGN", "COMPOSITE")
	b.WriteString("  " + strings.Repeat("-", 70) + "\n")
	for i, co := range companies[:12] {
		env := 65 + i*2%25
		soc := 70 + i*3%20
		gov := 75 + i%15
		align := 60 + i*4%30
		comp := (env + soc + gov + align) / 4
		fmt.Fprintf(b, "  %-28s %8d %8d %8d %8d %8d\n", co, env, soc, gov, align, comp)
	}
	b.WriteString("\n")
}

func riskSection(b *strings.Builder) {
	section(b, "Risk Management Framework")
	b.WriteString("The firm's enterprise risk management framework, overseen by Tomasz Kowalczyk, classifies risks across five categories: Market Risk, Liquidity Risk, Operational Risk, Regulatory Risk, and Reputational Risk.  Each portfolio company is assigned a risk rating (1–5) that is reviewed quarterly.\n\n")
	b.WriteString("Tomasz Kowalczyk presented the annual risk review to the Investment Committee on November 18, 2024.  The review highlighted three companies on the elevated-monitoring list and two macro-level risks (rising interest rates and geopolitical uncertainty in Eastern European markets) classified as Level 4 — High.\n\n")

	section(b, "Portfolio Risk Ratings — Q4 2024")
	fmt.Fprintf(b, "  %-28s %8s %8s %8s %8s %8s\n", "COMPANY", "MARKET", "LIQUIDITY", "OPS", "REG", "COMPOSITE")
	b.WriteString("  " + strings.Repeat("-", 75) + "\n")
	for i, co := range companies {
		market := 1 + i%5
		liq := 1 + (i+1)%5
		ops := 1 + (i+2)%4
		reg := 1 + (i+3)%3
		comp := (market + liq + ops + reg + 1) / 2
		if comp > 5 {
			comp = 5
		}
		fmt.Fprintf(b, "  %-28s %8d %8d %8d %8d %8d\n", co, market, liq, ops, reg, comp)
	}
	b.WriteString("\n")
}

func lpSection(b *strings.Builder) {
	section(b, "LP Base Overview")
	lpTypes := []string{
		"Sovereign Wealth Funds", "Public Pension Funds", "Insurance Companies",
		"Endowments and Foundations", "Family Offices", "Fund of Funds", "Corporate Investors",
	}
	b.WriteString("Meridian's LP base comprises 94 distinct limited partners across seven categories.\n\n")
	for _, lp := range lpTypes {
		pct := 8 + len(lp)%18
		fmt.Fprintf(b, "  %-34s %d%%\n", lp, pct)
	}
	b.WriteString("\n")

	section(b, "Capital Activity — FY2024")
	b.WriteString("Alejandro Torres presented the capital activity summary to the Investment Committee at the December 2024 meeting.  Total capital called during fiscal 2024 was $680M across six funds.  Total capital distributed was $420M, including the proceeds from four full exits and two secondary sales.\n\n")
	b.WriteString("The firm's largest distribution in 2024 was the $180M return of capital and profit following the sale of ArborTech to a strategic acquirer at 3.8x MOIC.  Robert Ashworth described the exit as 'a textbook example of the firm's patient capital approach'.\n\n")
}

func outlookSection(b *strings.Builder) {
	section(b, "Strategic Priorities 2025–2027")
	priorities := []string{
		"Close Meridian Growth Fund V at target size of $1.2 billion, with a first close targeted for Q3 2025.  Priscilla Fontaine is leading LP outreach alongside Robert Ashworth.",
		"Launch Meridian Infrastructure Fund III with a $900M target, incorporating a dedicated digital infrastructure sleeve.  Kweku Mensah and Marcus Delacroix will co-lead the fund.",
		"Establish an Asia-Pacific office in Singapore, led by Yuki Tanaka, to originate and execute investments in high-growth technology companies across Southeast Asia, Japan, and South Korea.",
		"Complete the rollout of the firm's proprietary portfolio management platform, MemOS, which will centralise company data, board materials, KPI tracking, and LP reporting.  Ingrid Nystrom is overseeing the technology implementation.",
		"Achieve SFDR Article 9 classification for Meridian Climate Opportunities Fund II, targeted for launch in Q1 2026.  Sophie Leclercq is leading the product design and regulatory engagement.",
		"Expand the firm's credit capabilities through a strategic hire of a dedicated credit partner.  Helena Voss is managing the search process with two short-listed candidates.",
		"Implement a comprehensive diversity, equity, and inclusion (DEI) programme, including targets for investment team composition.  Fatima Al-Rashid and Diana Ostrowski are co-chairing the DEI working group.",
	}
	for i, p := range priorities {
		fmt.Fprintf(b, "%d. %s\n\n", i+1, p)
	}
}

func biographies(b *strings.Builder) {
	bios := []struct {
		name, bio string
	}{
		{"Robert Ashworth", "Robert Ashworth is the Managing Partner and co-founder of Meridian Capital Partners.  He has over 28 years of experience in private equity and venture capital.  Prior to founding Meridian in 2008, Robert served as a Managing Director at Goldman Sachs Private Equity and as a Partner at Apax Partners.  He holds an MBA from Harvard Business School and a BA in Economics from the University of Oxford.  Robert chairs the Investment Committee and serves on the boards of QuantumHealth, NovaPay, and MeridianAI."},
		{"Priscilla Fontaine", "Priscilla Fontaine is a General Partner at Meridian Capital Partners, leading the Growth Equity practice.  She joined Meridian in 2012 from Sequoia Capital, where she focused on enterprise software and FinTech investments.  Priscilla holds an MBA from INSEAD and a BSc in Computer Science from ETH Zurich.  She leads Meridian's investment in NovaPay and serves on the boards of PulseGrid and DataForge."},
		{"Kweku Mensah", "Kweku Mensah is a General Partner at Meridian Capital Partners, heading the Infrastructure and Climate practice.  He previously served as Head of Infrastructure Investments at the Abu Dhabi Investment Authority (ADIA) and as a Director at Macquarie Infrastructure.  Kweku holds an MSc in Engineering from Imperial College London and an MBA from London Business School.  He serves on the boards of IronBridge Infrastructure, CarbonBridge, and TerraFund."},
		{"Yuki Tanaka", "Yuki Tanaka is a Managing Director at Meridian Capital Partners with responsibility for Asia-Pacific expansion.  He joined Meridian in 2015 following eight years at SoftBank Vision Fund and Recruit Holdings.  Yuki holds a BA from the University of Tokyo and an MBA from Wharton.  He serves on the boards of MeridianAI, ApexSemi, and ZephyrData."},
		{"Diana Ostrowski", "Diana Ostrowski is a Partner at Meridian Capital Partners covering Consumer Technology and Healthcare.  She previously worked at TPG Capital and McKinsey & Company's healthcare practice.  Diana holds an MD from Johns Hopkins School of Medicine and an MBA from the Stanford Graduate School of Business.  She led Meridian's investment in QuantumHealth and serves on the boards of CoastalMed and BioNexus."},
		{"Alejandro Torres", "Alejandro Torres is CFO and a Partner at Meridian Capital Partners.  He oversees financial reporting, fund administration, tax structuring, and operational finance.  Prior to joining Meridian in 2013, Alejandro held CFO roles at two private equity-backed businesses and spent six years at Deloitte's financial services audit practice.  He holds a BA in Accounting from the University of Barcelona and is a Chartered Financial Analyst (CFA)."},
		{"Sophie Leclercq", "Sophie Leclercq is a Partner and Head of ESG and Impact Measurement at Meridian Capital Partners.  She joined Meridian in 2019 from the International Finance Corporation (IFC), where she managed a portfolio of sustainable infrastructure investments across Sub-Saharan Africa.  Sophie holds an MSc in Environmental Economics from the London School of Economics.  She chairs Meridian's ESG Committee and represents the firm at the UNPRI Advisory Council."},
		{"Tomasz Kowalczyk", "Tomasz Kowalczyk is a Partner and Chief Risk Officer at Meridian Capital Partners.  He is responsible for enterprise risk management, portfolio company monitoring, and stress testing of fund models.  Prior to joining Meridian in 2016, Tomasz spent twelve years at Deutsche Bank's risk management division, including four years as Head of Credit Risk for European leveraged finance.  He holds an MSc in Mathematical Finance from Oxford and a BSc in Mathematics from Warsaw University."},
		{"Helena Voss", "Helena Voss is a Partner at Meridian Capital Partners, leading the Credit Opportunities and Special Situations strategy.  She previously served as Head of Distressed Debt at BlueMountain Capital Management and as a Managing Director in Goldman Sachs' restructuring group.  Helena holds a JD from Yale Law School and a BA in Economics from Princeton University."},
		{"Marcus Delacroix", "Marcus Delacroix was promoted to Partner in January 2025, having joined Meridian as a Principal in 2020.  He focuses on infrastructure and renewables investments, working alongside Kweku Mensah.  Marcus previously worked at Ardian Infrastructure and Lazard's infrastructure advisory team.  He holds an MSc in Electrical Engineering from École Polytechnique and an MBA from Columbia Business School."},
		{"Ingrid Nystrom", "Ingrid Nystrom joined Meridian Capital Partners as Partner, Technology Sector, in September 2024.  She previously served as VP of Investments at Index Ventures and as Head of Corporate Development at Klarna.  Ingrid holds an MSc in Information Systems from Stockholm School of Economics.  She leads Meridian's investment in FractalRobotics and ApexSemi."},
		{"Basil Okonkwo", "Basil Okonkwo is a Partner at Meridian Capital Partners, responsible for investments in Africa and Emerging Markets.  He joined Meridian in 2018 from the African Development Bank, where he managed a $400M equity portfolio.  Basil holds an MSc in Development Finance from Oxford and an LLB from the University of Lagos."},
	}
	for _, bio := range bios {
		section(b, bio.name)
		b.WriteString(bio.bio + "\n\n")
	}
}

func glossary(b *strings.Builder) {
	terms := [][2]string{
		{"AUM", "Assets Under Management — the total market value of investments managed by a fund on behalf of its limited partners."},
		{"IRR", "Internal Rate of Return — the annualised rate of return on an investment, accounting for the timing of cash flows."},
		{"MOIC", "Multiple on Invested Capital — the ratio of total value (realised and unrealised) to the total capital invested."},
		{"DPI", "Distributed to Paid-In — the ratio of capital returned to limited partners to the total capital called from them."},
		{"RVPI", "Residual Value to Paid-In — the ratio of the current fair value of unrealised investments to total capital called."},
		{"TVPI", "Total Value to Paid-In — the sum of DPI and RVPI, representing total value created relative to capital invested."},
		{"LP", "Limited Partner — an investor in a private equity fund who provides capital but does not participate in day-to-day management."},
		{"GP", "General Partner — the fund manager who makes investment decisions and manages the fund on behalf of limited partners."},
		{"Carry / Carried Interest", "The share of profits (typically 20%) that the General Partner receives once limited partners have received their invested capital plus a preferred return."},
		{"Hurdle Rate", "The minimum rate of return (typically 8%) that a fund must achieve before the General Partner is entitled to receive carried interest."},
		{"Clawback", "A provision requiring the General Partner to return carried interest previously received if the fund ultimately underperforms the hurdle rate on a cumulative basis."},
		{"NAV", "Net Asset Value — the fair value of all assets in a fund, net of liabilities, at a given point in time."},
		{"PIK", "Payment in Kind — interest or dividends paid in additional securities rather than cash."},
		{"Vintage Year", "The year in which a fund makes its first capital call or closes on its first investment."},
		{"Dry Powder", "Committed but uncalled capital available for future investments."},
		{"Co-Investment", "An investment made by a limited partner alongside the fund in a specific portfolio company, typically on more favourable terms."},
		{"Secondary", "The purchase or sale of existing private equity fund interests or portfolio company stakes in the secondary market."},
		{"SFDR", "Sustainable Finance Disclosure Regulation — EU regulation requiring financial market participants to disclose how sustainability risks are integrated into investment decisions."},
		{"UNPRI", "United Nations Principles for Responsible Investment — a voluntary framework for incorporating ESG factors into investment analysis and decision-making."},
		{"ESG", "Environmental, Social, and Governance — a framework for assessing the sustainability and ethical impact of an investment or business."},
		{"Cap Table", "Capitalisation Table — a spreadsheet or document showing the equity ownership structure of a company, including all shareholders and option holders."},
		{"Pre-Money Valuation", "The valuation of a company before a new round of funding is added, used to calculate the price per share for the new investors."},
		{"Post-Money Valuation", "The valuation of a company after new capital has been invested, equal to the pre-money valuation plus the new investment."},
		{"Anti-Dilution", "A provision protecting existing investors from dilution in future financing rounds that value the company lower than the current round."},
		{"Liquidation Preference", "A clause specifying the order and amount in which investors are paid in the event of a liquidation, sale, or IPO."},
		{"Tag-Along", "A contractual right allowing minority shareholders to join a transaction if majority shareholders sell their stake."},
		{"Drag-Along", "A right allowing majority shareholders to force minority shareholders to join in the sale of the company."},
		{"EBITDA", "Earnings Before Interest, Taxes, Depreciation, and Amortisation — a measure of a company's core operating profitability."},
		{"ARR", "Annual Recurring Revenue — the value of the recurring revenue components of a subscription business, normalised to a one-year period."},
		{"NRR", "Net Revenue Retention — a metric measuring revenue expansion from existing customers, accounting for churn, contraction, and expansion."},
	}
	for _, term := range terms {
		fmt.Fprintf(b, "%s\n    %s\n\n", term[0], term[1])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PDF generator — pure Go, no external deps
// Produces a valid PDF/1.4 document with embedded text content.
// Each "page" holds a chunk of text using the Helvetica base font.
// ─────────────────────────────────────────────────────────────────────────────

// buildPDF delegates to buildPDFCorrect.
func buildPDF(text string) []byte { return buildPDFCorrect(text) }

// buildPDFCorrect generates a correct PDF in a single pass by pre-computing
// all object numbers before writing.
func buildPDFCorrect(text string) []byte {
	var (
		pageW    float64 = 595.28
		pageH    float64 = 841.89
		margin   float64 = 50.0
		fontSize float64 = 9.0
		lineH    float64 = 13.0
	)
	_ = pageW

	linesPerPage := int((pageH - 2*margin) / lineH)

	rawLines := strings.Split(text, "\n")
	var lines []string
	for _, l := range rawLines {
		if len(l) <= 100 {
			lines = append(lines, l)
			continue
		}
		for len(l) > 100 {
			cut := 100
			for cut > 70 && l[cut] != ' ' {
				cut--
			}
			if cut == 70 {
				cut = 100
			}
			lines = append(lines, l[:cut])
			l = l[cut:]
		}
		if len(l) > 0 {
			lines = append(lines, l)
		}
	}

	var pageChunks [][]string
	for len(lines) > 0 {
		end := linesPerPage
		if end > len(lines) {
			end = len(lines)
		}
		pageChunks = append(pageChunks, lines[:end])
		lines = lines[end:]
	}

	nPages := len(pageChunks)

	// Object number layout:
	//   1        = Catalog
	//   2        = Pages tree
	//   3        = Font
	//   4 .. 3+nPages     = Content streams
	//   4+nPages .. 3+2*nPages = Page objects

	catalogObj := 1
	pagesObj := 2
	fontObj := 3
	firstStreamObj := 4
	firstPageObj := 4 + nPages

	totalObjs := 3 + 2*nPages

	// Pre-build all content streams so we know their lengths.
	type streamData struct {
		data []byte
	}
	streams := make([]streamData, nPages)
	for i, chunk := range pageChunks {
		var s bytes.Buffer
		fmt.Fprintf(&s, "BT\n/F1 %.1f Tf\n", fontSize)
		y := pageH - margin - fontSize
		for _, line := range chunk {
			safe := pdfEscape(line)
			fmt.Fprintf(&s, "%.2f %.2f Td\n(%s) Tj\n", margin, y, safe)
			y -= lineH
		}
		s.WriteString("ET\n")
		streams[i] = streamData{data: s.Bytes()}
	}

	var buf bytes.Buffer
	offsets := make([]int, totalObjs+1) // 1-indexed

	wf := func(format string, args ...interface{}) { fmt.Fprintf(&buf, format, args...) }
	ws := func(s string) { buf.WriteString(s) }
	wb := func(b []byte) { buf.Write(b) }

	ws("%PDF-1.4\n")
	ws("%\xe2\xe3\xcf\xd3\n")

	// Obj 1: Catalog
	offsets[catalogObj] = buf.Len()
	wf("%d 0 obj\n<< /Type /Catalog /Pages %d 0 R >>\nendobj\n", catalogObj, pagesObj)

	// Obj 2: Pages tree
	offsets[pagesObj] = buf.Len()
	wf("%d 0 obj\n<< /Type /Pages /Kids [", pagesObj)
	for i := 0; i < nPages; i++ {
		wf("%d 0 R ", firstPageObj+i)
	}
	wf("] /Count %d >>\nendobj\n", nPages)

	// Obj 3: Font
	offsets[fontObj] = buf.Len()
	wf("%d 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>\nendobj\n", fontObj)

	// Content stream objects
	for i, sd := range streams {
		objNum := firstStreamObj + i
		offsets[objNum] = buf.Len()
		wf("%d 0 obj\n<< /Length %d >>\nstream\n", objNum, len(sd.data))
		wb(sd.data)
		ws("\nendstream\nendobj\n")
	}

	// Page objects
	for i := 0; i < nPages; i++ {
		objNum := firstPageObj + i
		offsets[objNum] = buf.Len()
		wf("%d 0 obj\n", objNum)
		wf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.2f %.2f]\n", pagesObj, pageW, pageH)
		wf("   /Contents %d 0 R\n", firstStreamObj+i)
		ws("   /Resources << /Font << /F1 3 0 R >> >> >>\n")
		ws("endobj\n")
	}

	// Cross-reference table
	xrefStart := buf.Len()
	wf("xref\n0 %d\n", totalObjs+1)
	ws("0000000000 65535 f \n")
	for i := 1; i <= totalObjs; i++ {
		wf("%010d 00000 n \n", offsets[i])
	}
	wf("trailer\n<< /Size %d /Root %d 0 R >>\n", totalObjs+1, catalogObj)
	wf("startxref\n%d\n%%%%EOF\n", xrefStart)

	return buf.Bytes()
}

// ─────────────────────────────────────────────────────────────────────────────
// boardMeetingSummary writes a board meeting summary for a portfolio company
// in a given year (four meetings per year, 3–4 paragraphs each).
// ─────────────────────────────────────────────────────────────────────────────
func boardMeetingSummary(b *strings.Builder, company string, year int) {
	months := []string{"March", "June", "September", "December"}
	for m, month := range months {
		idx := (len(company) + m + year) % len(partners)
		chair := partners[idx]
		var attendees []string
		for k := 0; k < 4; k++ {
			attendees = append(attendees, partners[(idx+k)%len(partners)])
		}
		section(b, fmt.Sprintf("%s Board Meeting — %s %d", company, month, year))
		fmt.Fprintf(b, "Date:       %s %d, %d\n", month, 10+m*3, year)
		fmt.Fprintf(b, "Chair:      %s\n", chair)
		fmt.Fprintf(b, "Attendees:  %s\n\n", strings.Join(attendees, ", "))

		ceo := []string{"Alexandra Brandt", "Chen Wei", "Nadia Petrov", "Samuel Osei",
			"Laura Bergmann", "Carlos Mendez", "Anya Sharma", "Patrick O'Brien"}[(len(company)+m)%8]
		rev := 8 + m*4 + year - 2022
		growth := 35 + m*10 + (year-2022)*15

		fmt.Fprintf(b, "%s opened the board meeting and confirmed that the company had achieved $%dM in annualised recurring revenue at the end of Q%d %d, representing %d%% growth year-on-year.  CEO %s presented the business update, covering product roadmap, sales pipeline, customer retention, and headcount.\n\n",
			chair, rev, m+1, year, growth, ceo)

		fmt.Fprintf(b, "The board reviewed and approved the management accounts for the quarter.  Gross margin for the period was %d%%, and the company maintained a cash runway of %d months at the current burn rate.  %s noted that the company is on track to reach profitability in Q%d %d if growth targets are met.\n\n",
			62+m*2, 18+m*3, chair, (m+2)%4+1, year+1)

		fmt.Fprintf(b, "The board approved a revised hiring plan, adding %d new engineering roles and %d sales roles in the following quarter.  The compensation committee, chaired by %s, approved updated stock option grants for three senior hires joining in %s %d.\n\n",
			5+m*2, 3+m, attendees[1%len(attendees)], months[(m+1)%4], year)

		fmt.Fprintf(b, "Action items for the next board meeting: management to provide a detailed competitive analysis, %s to circulate revised financial model incorporating updated macro assumptions, and the CEO to present a three-year strategic plan by the %s %d meeting.\n\n",
			chair, months[(m+1)%4], year)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// lpUpdateLetter writes a quarterly LP update letter for a given year/quarter.
// ─────────────────────────────────────────────────────────────────────────────
func lpUpdateLetter(b *strings.Builder, year, quarter int) {
	months := []string{"", "January", "April", "July", "October"}
	section(b, fmt.Sprintf("LP Update Letter — Q%d %d", quarter, year))
	fmt.Fprintf(b, "Date: %s 15, %d\n", months[quarter], year)
	fmt.Fprintf(b, "From: Robert Ashworth, Managing Partner\n")
	fmt.Fprintf(b, "To:   All Limited Partners of Meridian Capital Partners\n\n")

	irr := 17.0 + float64(quarter)*0.5 + float64(year-2022)*0.8
	aum := 5.2 + float64(quarter)*0.2 + float64(year-2022)*0.5
	deployed := 80 + quarter*30 + (year-2022)*50
	co1 := companies[(year*4+quarter)%len(companies)]
	co2 := companies[(year*4+quarter+3)%len(companies)]
	co3 := companies[(year*4+quarter+7)%len(companies)]
	p1 := partners[(year*4+quarter)%len(partners)]
	p2 := partners[(year*4+quarter+5)%len(partners)]

	fmt.Fprintf(b, "Dear Limited Partners,\n\n")
	fmt.Fprintf(b, "I am pleased to provide you with the Meridian Capital Partners quarterly update for Q%d %d.  The firm continues to execute against its investment strategy and deliver strong risk-adjusted returns for our LP base.\n\n", quarter, year)
	fmt.Fprintf(b, "Portfolio Performance: The blended net IRR across all active funds stands at %.1f%% as of %s 30, %d, with total assets under management of $%.1f billion.  Capital deployed in Q%d %d amounted to $%dM across three new investments and two follow-on rounds.\n\n",
		irr, months[quarter], year, aum, quarter, year, deployed)
	fmt.Fprintf(b, "Portfolio Highlights: %s, led by %s, achieved a significant commercial milestone by closing its first enterprise contract with a Fortune 500 client, bringing total ARR to $%dM.  This represents a validation of our original investment thesis and positions the company well for its next financing round.\n\n",
		co1, p1, 12+quarter*8+(year-2022)*20)
	fmt.Fprintf(b, "%s received a follow-on investment of $%dM in Q%d, led by %s working alongside the management team.  The investment values the company at $%dM post-money and extends the cash runway to 30 months.  Meridian's ownership is %.1f%% on a fully diluted basis following the round.\n\n",
		co2, 15+quarter*5, quarter, p2, 120+quarter*40+(year-2022)*60, 14.0+float64(quarter)*0.3)
	fmt.Fprintf(b, "ESG Update: Sophie Leclercq reports that %s achieved its Scope 1 and Scope 2 carbon neutrality target in Q%d %d, ahead of schedule.  The firm's aggregate portfolio carbon intensity declined by %d%% year-to-date, on track to meet the annual target.\n\n",
		co3, quarter, year, 8+quarter*2)
	fmt.Fprintf(b, "Capital Activity: Total capital called in Q%d %d was $%dM.  Total distributions to LPs during the quarter amounted to $%dM, including the partial realisation of the firm's position in one portfolio company through a secondary sale.  Cumulative DPI across the flagship funds stands at %.2fx.\n\n",
		quarter, year, deployed, deployed/3, 0.3+float64(quarter)*0.05+float64(year-2022)*0.1)
	fmt.Fprintf(b, "We remain confident in the quality of the portfolio and the firm's ability to generate superior returns over the long term.  The next Investment Committee meeting is scheduled for %s %d and will cover a new investment proposal as well as the annual fund performance review.\n\n",
		months[quarter%4+1], year)
	fmt.Fprintf(b, "As always, please do not hesitate to contact Alejandro Torres (CFO) or your dedicated investor relations contact with any questions.\n\n")
	fmt.Fprintf(b, "Yours sincerely,\n\nRobert Ashworth\nManaging Partner, Meridian Capital Partners\n\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// valuationSection writes valuation methodology and per-company audit tables.
// ─────────────────────────────────────────────────────────────────────────────
func valuationSection(b *strings.Builder) {
	section(b, "Valuation Policy Overview")
	paras := []string{
		"Meridian Capital Partners measures the fair value of all portfolio investments in accordance with ASC 820 (Fair Value Measurement) and IPEV Valuation Guidelines (December 2022 edition).  Valuations are prepared quarterly by the finance team under the supervision of Alejandro Torres and reviewed independently by the firm's external auditor, KPMG LLP.",
		"All Level 3 fair value measurements (privately held equity and debt instruments without observable market prices) are reviewed by the Valuation Committee, which comprises Alejandro Torres (chair), Tomasz Kowalczyk, Helena Voss, and an independent valuation advisor from FTI Consulting.  The Valuation Committee meets within 30 days of each quarter-end.",
		"Primary valuation methodologies applied are: (1) Revenue Multiple — applied to high-growth software and technology companies using a peer group of 8–12 publicly traded comparables; (2) EBITDA Multiple — applied to infrastructure and services businesses; (3) Discounted Cash Flow (DCF) — applied where long-dated contracted cash flows exist; (4) Cost Minus Impairment — applied to early-stage companies with limited trading history.",
		"Where multiple methodologies are applied, the Valuation Committee exercises judgement in weighting the approaches, taking into account the stage, sector, and specific circumstances of each investment.  Material deviations from prior-quarter valuations require a written memorandum from the lead partner.",
		"The Valuation Committee specifically reviewed the Level 3 inputs applied to DataForge, ZephyrData, and VaultChain in Q4 2024, given the deterioration in comparable company multiples and the balance-sheet stress identified in each company.  Downward adjustments were approved for all three companies following Tomasz Kowalczyk's risk review.",
	}
	for _, p := range paras {
		b.WriteString(p + "\n\n")
	}

	section(b, "Level Classification Summary — Q4 2024")
	fmt.Fprintf(b, "  %-28s %-10s %-22s %12s %12s\n", "COMPANY", "LEVEL", "PRIMARY METHOD", "COST ($M)", "FAIR VALUE ($M)")
	b.WriteString("  " + strings.Repeat("-", 85) + "\n")
	methods := []string{"Revenue Multiple", "EBITDA Multiple", "DCF", "Revenue Multiple", "Cost Minus Impairment", "Revenue Multiple", "EBITDA Multiple", "DCF"}
	for i, co := range companies {
		level := 3
		if i%7 == 0 {
			level = 2
		}
		cost := 15 + i*12
		fv := cost + cost*2/5 + i*6
		if i == 3 || i == 9 || i == 18 { // write-downs
			fv = cost * 6 / 10
		}
		fmt.Fprintf(b, "  %-28s Level %-4d %-22s %12d %12d\n", co, level, methods[i%len(methods)], cost, fv)
	}
	b.WriteString("\n")

	section(b, "Revenue Multiple Peer Groups")
	peerGroups := []struct {
		sector    string
		median    float64
		companies []string
	}{
		{"Enterprise SaaS", 8.5, []string{"Salesforce", "ServiceNow", "Workday", "HubSpot", "Zendesk", "Veeva", "Coupa", "Sprinklr"}},
		{"FinTech", 11.2, []string{"Adyen", "Stripe (priv.)", "Wise", "Marqeta", "Brex", "Checkout.com", "Klarna (priv.)", "Nuvei"}},
		{"HealthTech", 7.8, []string{"Doximity", "Health Catalyst", "Phreesia", "Evolent", "Accolade", "Privia Health", "SOC Telemed", "GoodRx"}},
		{"CleanTech", 14.3, []string{"Enphase", "SolarEdge", "Array Technologies", "Shoals Technologies", "Stem", "Altus Power", "Fluence Energy", "Nextracker"}},
	}
	for _, pg := range peerGroups {
		fmt.Fprintf(b, "%s Peer Group — Median Revenue Multiple: %.1fx\n", pg.sector, pg.median)
		fmt.Fprintf(b, "  Comparable companies: %s\n\n", strings.Join(pg.companies, ", "))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// complianceSection writes the legal and compliance review.
// ─────────────────────────────────────────────────────────────────────────────
func complianceSection(b *strings.Builder) {
	section(b, "Legal and Regulatory Overview")
	paras := []string{
		"James Whitfield, General Counsel and Partner, oversees all legal and regulatory matters for Meridian Capital Partners and its managed funds.  Fatima Al-Rashid, Partner, provides additional regulatory expertise in MENA jurisdictions and Islamic finance structures.  The firm retains external legal counsel in all key jurisdictions including Linklaters (London), Latham & Watkins (New York), and Allen & Gledhill (Singapore).",
		"Meridian Capital Partners (UK) LLP is authorised and regulated by the Financial Conduct Authority (FCA) under the Alternative Investment Fund Managers Directive (AIFMD).  The firm holds permissions for managing alternative investment funds (AIFs) and advising on investments.  The annual AIFMD transparency report for fiscal 2024 was filed by James Whitfield on March 31, 2025.",
		"The EU-domiciled funds (Meridian Growth Fund IV SCSP and Meridian Infrastructure Fund II SCSP, both Luxembourg-domiciled) are subject to AIFMD as implemented in Luxembourg by the CSSF.  Fatima Al-Rashid coordinated with the Luxembourg-based AIFM, Meridian Management S.A., to ensure full AIFMD compliance.",
		"Under SFDR, Meridian Climate Opportunities Fund I is classified as an Article 9 fund (dark green), and Meridian Growth Fund V will be classified as Article 8 upon launch.  Sophie Leclercq and James Whitfield jointly prepared the Principal Adverse Impact (PAI) statement and periodic reports for the Article 9 fund.",
		"AML/KYC: All new LP investors undergo enhanced due diligence in accordance with the firm's AML/KYC policy, last updated in October 2024 to reflect the EU's 6th Anti-Money Laundering Directive (6AMLD).  Fatima Al-Rashid chairs the AML Committee, which reviews high-risk investor cases.  No suspicious activity reports were filed in fiscal 2024.",
		"James Whitfield managed three LP side-letter requests during fiscal 2024, relating to ERISA compliance, MFN provisions, and transparency reporting obligations for a public pension fund investor.  All side-letter terms were reviewed by the Investment Committee and determined to be consistent with fund governance.",
	}
	for _, p := range paras {
		b.WriteString(p + "\n\n")
	}

	section(b, "Regulatory Filings — FY2024")
	filings := []struct {
		filing, regulator, date, officer string
	}{
		{"AIFMD Annual Transparency Report (UK)", "FCA", "March 31, 2024", "James Whitfield"},
		{"AIFMD Annex IV Report — Q1 2024", "FCA / CSSF", "April 30, 2024", "James Whitfield"},
		{"SFDR PAI Statement FY2023", "CSSF", "June 30, 2024", "Sophie Leclercq"},
		{"AIFMD Annex IV Report — Q2 2024", "FCA / CSSF", "July 31, 2024", "James Whitfield"},
		{"Form ADV Annual Update", "SEC", "March 31, 2024", "Alejandro Torres"},
		{"AIFMD Annex IV Report — Q3 2024", "FCA / CSSF", "October 31, 2024", "James Whitfield"},
		{"CRS/FATCA Annual Reporting", "HMRC / IRS", "May 31, 2024", "Alejandro Torres"},
		{"AIFMD Annual Transparency Report (LU)", "CSSF", "March 31, 2024", "Fatima Al-Rashid"},
		{"AIFMD Annex IV Report — Q4 2024", "FCA / CSSF", "January 31, 2025", "James Whitfield"},
	}
	fmt.Fprintf(b, "  %-44s %-18s %-14s %s\n", "FILING", "REGULATOR", "DATE", "RESPONSIBLE OFFICER")
	b.WriteString("  " + strings.Repeat("-", 95) + "\n")
	for _, f := range filings {
		fmt.Fprintf(b, "  %-44s %-18s %-14s %s\n", f.filing, f.regulator, f.date, f.officer)
	}
	b.WriteString("\n")

	section(b, "Litigation and Disputes")
	b.WriteString("As of March 31, 2025, Meridian Capital Partners is not a party to any material litigation.  James Whitfield confirmed that the minor contractual dispute with a former service provider, disclosed in the FY2023 Annual Report, was settled out of court in February 2024 for an amount that was not material to the firm.\n\n")
	b.WriteString("Fatima Al-Rashid disclosed a regulatory inquiry from the Dubai Financial Services Authority (DFSA) relating to marketing activities in the Dubai International Financial Centre (DIFC).  The inquiry is at an early stage and James Whitfield has engaged local counsel Clifford Chance LLP to respond.  The firm does not currently hold a DFSA licence and the inquiry relates to whether certain LP communications constituted a marketing communication under DIFC rules.\n\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// fundTermsSummary writes a table of fund terms for all active funds.
// ─────────────────────────────────────────────────────────────────────────────
func fundTermsSummary(b *strings.Builder) {
	section(b, "Fund Terms at a Glance")
	b.WriteString("The following table summarises the key economic and governance terms for each active Meridian fund.  Full terms are set out in each fund's Limited Partnership Agreement.\n\n")
	fmt.Fprintf(b, "  %-38s %8s %8s %8s %6s %8s %10s\n",
		"FUND", "TARGET", "MGMT FEE", "CARRY", "HURDLE", "GP COMMIT", "TERM (YRS)")
	b.WriteString("  " + strings.Repeat("-", 95) + "\n")
	type fundTerms struct {
		name, target, mgmt, carry, hurdle, gpCommit, term string
	}
	terms := []fundTerms{
		{"Meridian Growth Fund IV", "$700M", "2.0%", "20%", "8%", "2%", "10+2"},
		{"Meridian Growth Fund V", "$1,200M", "1.75%", "20%", "8%", "2%", "10+2"},
		{"Meridian Infrastructure Fund II", "$600M", "1.5%", "15%", "7%", "1.5%", "12+2"},
		{"Meridian Infrastructure Fund III", "$900M", "1.5%", "15%", "7%", "1.5%", "12+2"},
		{"Meridian Climate Opps Fund I", "$500M", "1.75%", "20%", "8%", "2%", "10+2"},
		{"Meridian Climate Opps Fund II", "$750M", "1.75%", "20%", "8%", "2%", "10+2"},
		{"Meridian Credit Opps Fund I", "$400M", "1.25%", "17.5%", "6%", "1.5%", "8+1"},
		{"Meridian Special Situations II", "$350M", "1.5%", "20%", "8%", "2%", "8+1"},
	}
	for _, t := range terms {
		fmt.Fprintf(b, "  %-38s %8s %8s %8s %6s %8s %10s\n",
			t.name, t.target, t.mgmt, t.carry, t.hurdle, t.gpCommit, t.term)
	}
	b.WriteString("\n")

	section(b, "Fee Calculations — Illustrative Example")
	b.WriteString("The following example illustrates the management fee and carried interest calculation for Meridian Growth Fund V, assuming a $1.2B close and a fund return of 2.3x MOIC (net of fees and carry).\n\n")
	b.WriteString("  Management Fee (investment period, on committed capital):\n")
	b.WriteString("    $1,200M × 1.75% = $21.0M per annum\n")
	b.WriteString("    Over 5-year investment period: $105.0M total management fees\n\n")
	b.WriteString("  Management Fee (post-investment period, on invested capital):\n")
	b.WriteString("    $1,020M (est. invested) × 1.0% = $10.2M per annum\n")
	b.WriteString("    Over 5-year harvest period: $51.0M additional management fees\n\n")
	b.WriteString("  Carried Interest (assuming 2.3x MOIC):\n")
	b.WriteString("    Gross proceeds: $2,760M\n")
	b.WriteString("    Return of capital: $1,200M\n")
	b.WriteString("    Preferred return (8% × 10 years): $1,200M × [(1.08)^10 - 1] ≈ $1,389M cumulative\n")
	b.WriteString("    Profits above hurdle: $2,760M - $1,200M - $589M preferred return = $971M\n")
	b.WriteString("    Carried interest (20%): $194.2M to General Partner\n")
	b.WriteString("    Net to LPs: $2,565.8M (after carry) = net MOIC of 2.14x on committed capital\n\n")

	section(b, "Key Person Provisions")
	b.WriteString("Each fund's constitutional documents include key person provisions triggered by the departure or material time reduction of designated key persons.  The following key persons are designated across the firm's active funds:\n\n")
	keyPersons := []struct {
		person, funds string
	}{
		{"Robert Ashworth", "All active funds — primary key person"},
		{"Priscilla Fontaine", "Meridian Growth Fund IV, Meridian Growth Fund V"},
		{"Kweku Mensah", "Meridian Infrastructure Fund II, Meridian Infrastructure Fund III, Meridian Climate Opportunities Fund I"},
		{"Marcus Delacroix", "Meridian Infrastructure Fund III (added January 2025)"},
		{"Sophie Leclercq", "Meridian Climate Opportunities Fund I, Meridian Climate Opportunities Fund II"},
		{"Helena Voss", "Meridian Credit Opportunities Fund I, Meridian Special Situations Fund II"},
	}
	for _, kp := range keyPersons {
		fmt.Fprintf(b, "  %-24s  %s\n", kp.person, kp.funds)
	}
	b.WriteString("\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// coInvestmentLog writes a log of co-investment transactions.
// ─────────────────────────────────────────────────────────────────────────────
func coInvestmentLog(b *strings.Builder) {
	section(b, "Co-Investment Programme Overview")
	paras := []string{
		"Meridian Capital Partners operates a co-investment programme under which certain qualifying limited partners may invest alongside the fund in specific portfolio companies, typically at a reduced or nil management fee and carry.  Co-investment rights are allocated on the basis of fund commitment size and prior co-investment participation.",
		"Robert Ashworth and Alejandro Torres oversee the co-investment programme.  James Whitfield maintains the co-investment register and ensures compliance with each fund's constitutional documents.  During fiscal 2024, the firm offered co-investment rights on five transactions totalling $340M, of which $280M was taken up by qualifying LPs.",
		"Co-investors are required to execute a Co-Investment Agreement and a side letter with each fund prior to closing their co-investment.  Fatima Al-Rashid reviews co-investment documentation for MENA-based LPs to ensure compliance with local regulatory requirements.",
	}
	for _, p := range paras {
		b.WriteString(p + "\n\n")
	}

	section(b, "Co-Investment Transaction Log — FY2022–FY2025 YTD")
	fmt.Fprintf(b, "  %-22s %-28s %-26s %10s %12s\n",
		"DATE", "COMPANY", "CO-INVESTOR", "AMOUNT ($M)", "LEAD PARTNER")
	b.WriteString("  " + strings.Repeat("-", 100) + "\n")
	type coInv struct {
		date, company, investor, amount, partner string
	}
	entries := []coInv{
		{"Mar 2022", "NovaPay", "Nordic Sovereign Wealth Fund", "$25M", "Priscilla Fontaine"},
		{"Mar 2022", "NovaPay", "European Insurance Co. A", "$15M", "Priscilla Fontaine"},
		{"Jun 2022", "IronBridge Infrastructure", "Gulf Pension Fund", "$40M", "Kweku Mensah"},
		{"Jun 2022", "IronBridge Infrastructure", "Canadian Pension Plan", "$35M", "Kweku Mensah"},
		{"Sep 2022", "CarbonBridge", "Impact Endowment Fund B", "$18M", "Sophie Leclercq"},
		{"Dec 2022", "QuantumHealth", "US University Endowment", "$22M", "Diana Ostrowski"},
		{"Feb 2023", "SkyLogistics", "Family Office Consortium C", "$12M", "Alejandro Torres"},
		{"Apr 2023", "MeridianAI", "European Tech Fund of Funds", "$8M", "Yuki Tanaka"},
		{"Jun 2023", "TerraFund", "African Development Finance Inst.", "$30M", "Basil Okonkwo"},
		{"Sep 2023", "PulseGrid", "Nordic Infrastructure Pension", "$20M", "Kweku Mensah"},
		{"Nov 2023", "FractalRobotics", "Deep Tech Family Office D", "$10M", "Ingrid Nystrom"},
		{"Jan 2024", "NovaPay", "Nordic Sovereign Wealth Fund", "$30M", "Priscilla Fontaine"},
		{"Mar 2024", "IronBridge Infrastructure", "Brookfield Asset Management", "$50M", "Marcus Delacroix"},
		{"May 2024", "QuantumHealth", "US Healthcare Foundation", "$18M", "Diana Ostrowski"},
		{"Jul 2024", "VaultChain", "Digital Assets Fund of Funds E", "$5M", "Dmitri Volkov"},
		{"Sep 2024", "CarbonBridge", "Climate Impact Pension Fund F", "$22M", "Sophie Leclercq"},
		{"Nov 2024", "ApexSemi", "Asia-Pacific Sovereign Fund", "$28M", "Yuki Tanaka"},
		{"Jan 2025", "FractalRobotics", "European Robotics Endowment", "$15M", "Ingrid Nystrom"},
		{"Feb 2025", "TerraFund", "Multilateral Development Bank G", "$35M", "Basil Okonkwo"},
		{"Mar 2025", "MeridianAI", "Global Tech Venture Fund H", "$12M", "Ingrid Nystrom"},
	}
	for _, e := range entries {
		fmt.Fprintf(b, "  %-22s %-28s %-26s %10s %12s\n",
			e.date, e.company, e.investor, e.amount, e.partner)
	}
	b.WriteString("\n")

	section(b, "Co-Investment Rights Allocation Policy")
	b.WriteString("Co-investment rights are offered to LPs in the following priority order:\n\n")
	b.WriteString("  Tier 1 (≥$100M commitment): Right to participate in all co-investments up to 15% of the total deal size.\n")
	b.WriteString("  Tier 2 ($50M–$99M commitment): Right to participate in co-investments up to 10% of the total deal size.\n")
	b.WriteString("  Tier 3 ($25M–$49M commitment): Right to participate in co-investments up to 5% of the total deal size on a first-come, first-served basis.\n")
	b.WriteString("  Tier 4 (<$25M commitment): Co-investment offered on a best-efforts basis subject to availability.\n\n")
	b.WriteString("Alejandro Torres distributes co-investment opportunity notices within 48 hours of Investment Committee approval of the primary transaction.  LPs have a 72-hour response window.  Unused allocation is first redistributed to other Tier 1 LPs and then down the tier structure.\n\n")
	b.WriteString("Robert Ashworth presented the co-investment programme review to the LP Advisory Committee on October 14, 2024.  The committee expressed strong support for the programme and requested that the minimum response window be extended to five business days for future co-investment opportunities, effective Q1 2025.  James Whitfield is updating the co-investment side letter template accordingly.\n\n")
}

// pdfEscape escapes characters that are special inside a PDF literal string.
func pdfEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		case r < 32 || r > 126:
			// Replace non-ASCII / control chars with space.
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
