package handlers

import "net/http"

type TrackGuide struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	ToolURL  string   `json:"tool_url"`
	CostNote string   `json:"cost_note"`
	Steps    []string `json:"steps"`
	Prompt   string   `json:"prompt"`
}

// Track content is returned only after checking the paid entitlement in storage.
func (d *Dashboard) Track(w http.ResponseWriter, r *http.Request) {
	id, err := d.authenticated(r)
	if err != nil {
		dashboardJSON(w, 401, map[string]string{"error": "sign in required"})
		return
	}
	var landing, video, verified bool
	if err = d.database.QueryRowContext(r.Context(), "SELECT tier1_landing,tier1_video,email_verified FROM students WHERE id=$1", id).Scan(&landing, &video, &verified); err != nil {
		dashboardJSON(w, 503, map[string]string{"error": "track unavailable"})
		return
	}
	track := r.PathValue("track")
	if track != "landing_pages" && track != "video" {
		http.NotFound(w, r)
		return
	}
	if !verified || (track == "landing_pages" && !landing) || (track == "video" && !video) {
		dashboardJSON(w, 403, map[string]string{"error": "purchase this learning track to continue"})
		return
	}
	guide := TrackGuide{ID: track, CostNote: "Tuition covers meuseAI learning guidance. External tool subscriptions, credits, hosting upgrades and premium assets are not included; check your tool account before generating or publishing."}
	if track == "landing_pages" {
		guide.Title = "Lovable landing pages"
		guide.ToolURL = "https://lovable.dev/"
		guide.Steps = []string{"Define one audience, one offer and one action. Write a headline, three benefits and a contact or signup call to action.", "Open Lovable, start a project and paste the starter brief below. Replace the bracketed details and upload an image you created and have permission to use directly in Lovable.", "Inspect the mobile and desktop previews. Ask for one improvement at a time: clearer contrast, shorter copy, then a stronger call to action. Check that buttons work and that no claims, testimonials or customer logos were invented.", "Preview the published version using Lovable’s current publishing controls. Test the contact or signup flow, page readability and image loading on your phone. Only publish forms or payment links you have configured and tested.", "Portfolio challenge: share the live landing-page link with a short explanation of the audience, offer and two improvements you made. Keep a copy of your final brief."}
		guide.Prompt = "Build a responsive one-page landing page for [business or project] serving [audience]. The offer is [offer]. Use my supplied hero image, a clear headline, three honest benefits and one primary action linking to [verified contact or signup URL]. Include a simple FAQ and footer. Use readable contrast, accessible labels and a clean mobile layout. Do not invent reviews, prices, payment processing or customer logos. Ask me for any missing business details before adding them."
	} else {
		guide.Title = "AI video generation with CapCut"
		guide.ToolURL = "https://www.capcut.com/tools/ai-video"
		guide.Steps = []string{"Plan a short vertical video with one message: a hook, a main scene and a closing action. Start with a 5–8 second clip to limit credit use. Decide which image from the free course you want to animate.", "Open CapCut and check whether your account offers AI video or image-to-video, the required credits, any subscription requirement and the export conditions. If generation is unavailable, practise the same storyboard by adding your still image to the editor and using keyframe motion.", "For image-to-video, attach an image you own or have permission to use in CapCut. Paste the starter prompt below, select a vertical format if available and generate one draft. Inspect faces, hands, object shape and background movement before spending more credits.", "Change only one motion instruction for a second draft. Choose the stronger result, trim it in CapCut and add a short readable caption. Use only music and assets whose licence permits your intended use; check the export preview for premium elements or watermarks.", "Export the short at a resolution your account supports. Review it on your phone with and without sound. Portfolio challenge: post your finished clip and explain the prompt, motion change and editing choices. This paid-track exercise does not automatically grant free-course sharing XP."}
		guide.Prompt = "Animate the supplied image into a 5–8 second cinematic clip. Keep the subject’s recognizable face, natural proportions, outfit and main object shapes consistent. Use a slow camera push-in with subtle realistic movement in hair, fabric and background light. Keep the subject in focus and avoid rapid cuts, new objects, added text or logos. Compose for a vertical 9:16 social post. For the second draft, change only the camera movement to a gentle left-to-right slide."
	}
	dashboardJSON(w, 200, guide)
}
