// Give the service worker access to Firebase Messaging.
// Note that you can only use Firebase Messaging here, other Firebase libraries
// are not available in the service worker.
importScripts('https://www.gstatic.com/firebasejs/7.9.2/firebase-app.js');
importScripts('https://www.gstatic.com/firebasejs/7.9.2/firebase-messaging.js');

// Initialize the Firebase app in the service worker by passing in the
// messagingSenderId.
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

// Retrieve an instance of Firebase Messaging so that it can handle background
// messages.
const messaging = firebase.messaging();

