package game

import (
	"fmt"

	fcm "github.com/zond/go-fcm"
	"google.golang.org/appengine/v2"
	"google.golang.org/appengine/v2/log"

	. "github.com/zond/goaeoas"
)

func init() {
	Handle(router, "/create-notification", []string{"POST"}, "CreateNotification", handleCreateNotification)
}

func handleCreateNotification(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	// Example FCM notification payload
	notificationPayload := &fcm.NotificationPayload{
		Title: "Test Notification",
		Body:  "This is a test notification sent via FCM.",
		Tag:   "test-notification",
	}

	// Example FCM data payload
	dataPayload := map[string]interface{}{
		"type": "test",
		"info": "This is a test data payload.",
	}

	// Replace with a valid FCM token for testing
	testFCMToken := "e-ifPVGU6xNdpT7At4KGuh:APA91bHadZg8nvdRfixmEMDDYgXXmWpVUiNTyKEW_Ue36hBfnhxIMkFUw0nXcVlfuaGJbU5OyVAlfFoaNZSO3rHRUVF7QXdtqdSJXFU8NJAshGYfr0Pjkj4"

	if err := FCMSendToTokensFunc.EnqueueIn(
		ctx,
		0,
		notificationPayload,
		dataPayload,
		map[string][]string{
			"test-user": {testFCMToken},
		},
	); err != nil {
		log.Errorf(ctx, "Failed to enqueue FCM notification: %v", err)
		return fmt.Errorf("failed to send notification: %w", err)
	}

	log.Infof(ctx, "Test notification enqueued successfully")
	return nil
}
