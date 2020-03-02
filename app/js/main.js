// Initialize Firebase
var firebaseConfig = {
  apiKey: "AIzaSyDxQpMuCYlu95_oG7FUCLFIYIIfvKz-4D8",
  authDomain: "diplicity-engine.firebaseapp.com",
  databaseURL: "https://diplicity-engine.firebaseio.com",
  projectId: "diplicity-engine",
  storageBucket: "diplicity-engine.appspot.com",
  messagingSenderId: "635122585664",
  appId: "1:635122585664:web:89244768f1d41245a74fa5"
};
firebase.initializeApp(firebaseConfig);

const messaging = firebase.messaging();
messaging.requestPermission().then(function() {
	console.log('Notification permission granted.');
	// Get Instance ID token. Initially this makes a network call, once retrieved
	// subsequent calls to getToken will return from cache.
	messaging.getToken()
	.then(function(currentToken) {
		if (currentToken) {
			if ($('#fcm-token').length == 0) {
				$('body').prepend('<div id="fcm-token" style="font-size: xx-small; font-weight: light;">Your FCM token: ' + currentToken + '</div>');
			} else {
				$('#fcm-token').text('Your FCM token: ' + currentToken);
			}
		} else {
			$('#fcm-token').remove();
		}
	})
	.catch(function(err) {
		console.log('An error occurred while retrieving token. ', err);
	});
	// Callback fired if Instance ID token is updated.
	messaging.onTokenRefresh(function() {
		messaging.getToken()
		.then(function(refreshedToken) {
			console.log('Token refreshed.');
		})
		.catch(function(err) {
			console.log('Unable to retrieve refreshed token ', err);
		});
	});
	// Handle incoming messages. Called when:
	// - a message is received while the app has focus
	// - the user clicks on an app notification created by a sevice worker
	//   'messaging.setBackgroundMessageHandler' handler.
	messaging.onMessage(function(payload) {
		console.log("Message received. ", payload);
		alert(payload.notification.title + '\n' + payload.notification.body);
		// ...
	});
}).catch(function(err) {
	console.log('Unable to get permission to notify.', err);
});
