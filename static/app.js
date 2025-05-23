document.addEventListener('DOMContentLoaded', () => {
    const ws = new WebSocket(`ws://${window.location.host}/ws`);
    const addDeviceBtn = document.getElementById('add-device');
    const qrSection = document.getElementById('qr-section');
    const qrCanvas = document.getElementById('qr-canvas');
    const devicesContainer = document.getElementById('devices-container');
    const deviceTemplate = document.getElementById('device-card-template');

    // Global state
    let currentDevice = null;
    const deviceMap = new Map(); // Use a map to track devices by JID

    // Functions page elements
    const functionsPage = document.getElementById('functions-page');
    const backToDevicesBtn = document.getElementById('back-to-devices');
    const currentDeviceName = document.getElementById('current-device-name');
    const sendMessageBtn = document.getElementById('send-message-btn');
    const messageStatus = document.getElementById('message-status');
    const fetchGroupsBtn = document.getElementById('fetch-groups-btn');
    const groupsList = document.getElementById('groups-list');

    let totalGroups = 0;
    let processedGroups = 0;
    let startTime = null;

    function updateGroupFetchProgress(data) {
        const progressCard = document.getElementById('group-fetch-progress');
        const progressBar = document.getElementById('group-fetch-progress-bar');
        const groupCount = document.getElementById('group-fetch-count');
        const groupTime = document.getElementById('group-fetch-time');
        const groupLog = document.getElementById('group-fetch-log');

        if (!progressCard.style.display || progressCard.style.display === 'none') {
            progressCard.style.display = 'block';
            startTime = new Date();
        }

        if (data.type === 'group-count') {
            totalGroups = data.count;
            processedGroups = 0;
            groupCount.textContent = `Found ${totalGroups} groups`;
            groupLog.innerHTML = ''; // Clear previous logs
        } else if (data.type === 'group-progress') {
            processedGroups++;
            const progress = (processedGroups / totalGroups) * 100;
            progressBar.style.width = `${progress}%`;
            
            // Add log entry
            const logEntry = document.createElement('div');
            logEntry.textContent = `${new Date().toLocaleTimeString()} - Adding group: ${data.name} with ${data.participants} participants`;
            groupLog.appendChild(logEntry);
            groupLog.scrollTop = groupLog.scrollHeight;

            // Update elapsed time
            const elapsed = Math.floor((new Date() - startTime) / 1000);
            groupTime.textContent = `Elapsed: ${elapsed}s`;
        } else if (data.type === 'group-complete') {
            progressBar.style.width = '100%';
            progressBar.classList.remove('progress-bar-animated');
            const elapsed = Math.floor((new Date() - startTime) / 1000);
            groupTime.textContent = `Completed in ${elapsed}s`;
        }
    }

    ws.onopen = () => {
        console.log('WebSocket connected');
        loadDevices();
    };

    ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        console.log('WebSocket message received:', data);
        
        if (data.type === 'status') {
            if (!data.deviceId) {
                console.error('Received status update without deviceId:', data);
                return;
            }
            
            console.log('Processing device status update:', {
                deviceId: data.deviceId,
                connected: data.connected,
                deviceInfo: data.deviceInfo
            });
            
            const deviceInfo = {
                id: data.deviceId,
                phoneNumber: data.deviceId.split('@')[0],
                pushName: data.deviceInfo?.pushName || 'Unknown Device',
                platform: data.deviceInfo?.platform || 'Unknown',
                connected: data.connected
            };
            
            updateDeviceStatus(data.deviceId, data.connected, deviceInfo);
        } else if (data.type === 'message') {
            console.log('Received message update:', data);
            if (currentDevice && currentDevice.id === data.deviceId) {
                showMessageStatus(`New message from ${data.from}: ${data.content}`);
            }
        } else if (data.type === 'group-count' || data.type === 'group-progress' || data.type === 'group-complete') {
            updateGroupFetchProgress(data);
        } else if (data.type === 'groups_total') {
            // Show total groups count
            document.getElementById('total-groups').classList.remove('hidden');
            document.getElementById('groups-count').textContent = data.count;
            console.log(`Found ${data.count} groups`);
        } else if (data.type === 'group_progress') {
            // Update groups list with new group
            const groupElement = document.createElement('div');
            groupElement.className = 'p-2 bg-gray-50 rounded';
            groupElement.innerHTML = `
                <div class="font-medium">${data.name}</div>
                <div class="text-sm text-gray-600">${data.participants} participants</div>
            `;
            groupsList.appendChild(groupElement);
        }
    };

    ws.onclose = () => {
        console.log('WebSocket disconnected');
        // Clear devices container and show reconnecting message
        devicesContainer.innerHTML = '';
        const reconnectingMsg = document.createElement('div');
        reconnectingMsg.className = 'col-span-full text-center p-4 text-yellow-500';
        reconnectingMsg.textContent = 'Connection lost. Reconnecting...';
        devicesContainer.appendChild(reconnectingMsg);
        
        // Attempt to reconnect after 2 seconds
        setTimeout(() => {
            window.location.reload();
        }, 2000);
    };

    async function loadDevices() {
        try {
            console.log('Loading devices from /devices endpoint...');
            const response = await fetch('/devices');
            const devices = await response.json();
            console.log('Loaded devices from server:', devices);
            
            // Clear existing devices
            devicesContainer.innerHTML = '';
            deviceMap.clear();
            
            if (!Array.isArray(devices)) {
                console.error('Invalid devices data:', devices);
                const errorMsg = document.createElement('div');
                errorMsg.className = 'col-span-full text-center p-4 text-red-500';
                errorMsg.textContent = 'Error: Invalid device data received';
                devicesContainer.appendChild(errorMsg);
                return;
            }
            
            if (devices.length === 0) {
                console.log('No devices found, showing empty state');
                const noDevicesMsg = document.createElement('div');
                noDevicesMsg.className = 'col-span-full text-center p-4 text-gray-500';
                noDevicesMsg.textContent = 'No devices connected. Click "Add New Device" to connect one.';
                devicesContainer.appendChild(noDevicesMsg);
                return;
            }
            
            devices.forEach(device => {
                // Handle both lowercase and uppercase JID field
                const jid = device.JID || device.jid;
                if (!jid) {
                    console.warn('Invalid device data:', device);
                    return;
                }
                
                console.log('Processing device:', device);
                const deviceInfo = {
                    id: jid,
                    phoneNumber: jid.split('@')[0],
                    pushName: device.PushName || device.pushName || 'Unknown Device',
                    platform: device.Platform || device.platform || 'Unknown',
                    connected: device.Connected || device.connected || false
                };
                
                console.log('Creating device with info:', deviceInfo);
                const deviceCard = createDeviceCard(jid, deviceInfo);
                deviceMap.set(jid, deviceCard);
                devicesContainer.appendChild(deviceCard);
            });
        } catch (error) {
            console.error('Error loading devices:', error);
            const errorMsg = document.createElement('div');
            errorMsg.className = 'col-span-full text-center p-4 text-red-500';
            errorMsg.textContent = 'Error loading devices. Please try refreshing the page.';
            devicesContainer.appendChild(errorMsg);
        }
    }

    function createDeviceCard(deviceId, deviceInfo) {
        console.log('Creating device card with info:', deviceInfo);
        const template = document.getElementById('device-card-template');
        const card = template.content.cloneNode(true).querySelector('.device-card');
        
        // Set device info
        card.querySelector('.device-name').textContent = deviceInfo.pushName || 'Unknown Device';
        card.querySelector('.phone-number').textContent = deviceInfo.phoneNumber || deviceId.split('@')[0] || 'Unknown';
        card.querySelector('.platform').textContent = deviceInfo.platform || 'Unknown';
        card.querySelector('.connection-status').textContent = deviceInfo.connected ? 'Connected' : 'Disconnected';
        
        const statusIndicator = card.querySelector('.status-indicator');
        statusIndicator.className = `status-indicator w-3 h-3 rounded-full mr-2 ${deviceInfo.connected ? 'bg-green-500' : 'bg-red-500'}`;
        
        // Set up button handlers
        const useFunctionsBtn = card.querySelector('.use-functions-btn');
        useFunctionsBtn.addEventListener('click', () => {
            showFunctionsPage({
                id: deviceId,
                name: deviceInfo.pushName || 'Unknown Device',
                phoneNumber: deviceInfo.phoneNumber || deviceId.split('@')[0] || 'Unknown',
                platform: deviceInfo.platform || 'Unknown'
            });
        });
        
        const logoutBtn = card.querySelector('.logout-btn');
        logoutBtn.addEventListener('click', () => handleLogout(deviceId));
        
        return card;
    }

    function updateDeviceStatus(deviceId, connected, deviceInfo) {
        console.log('Updating device status:', { deviceId, connected, deviceInfo });
        
        if (!deviceId) {
            console.error('Invalid deviceId:', deviceId);
            return;
        }
        
        let deviceCard = deviceMap.get(deviceId);
        
        if (!deviceCard && deviceInfo) {
            console.log('Creating new device card for:', deviceId);
            deviceCard = createDeviceCard(deviceId, {
                id: deviceId,
                phoneNumber: deviceId.split('@')[0],
                pushName: deviceInfo.pushName || deviceInfo.PushName || 'Unknown Device',
                platform: deviceInfo.platform || deviceInfo.Platform || 'Unknown',
                connected: connected
            });
            deviceMap.set(deviceId, deviceCard);
            
            // Remove "no devices" message if it exists
            const noDevicesMsg = devicesContainer.querySelector('.text-gray-500');
            if (noDevicesMsg) {
                noDevicesMsg.remove();
            }
            
            devicesContainer.appendChild(deviceCard);
        } else if (deviceCard) {
            console.log('Updating existing device card:', deviceId);
            const statusIndicator = deviceCard.querySelector('.status-indicator');
            statusIndicator.className = `status-indicator w-3 h-3 rounded-full mr-2 ${connected ? 'bg-green-500' : 'bg-red-500'}`;

            const connectionStatus = deviceCard.querySelector('.connection-status');
            connectionStatus.textContent = connected ? 'Connected' : 'Disconnected';

            if (deviceInfo) {
                deviceCard.querySelector('.device-name').textContent = deviceInfo.pushName || deviceInfo.PushName || 'Unknown Device';
                deviceCard.querySelector('.phone-number').textContent = deviceInfo.phoneNumber || deviceId.split('@')[0] || 'Unknown';
                deviceCard.querySelector('.platform').textContent = deviceInfo.platform || deviceInfo.Platform || 'Unknown';
            }
        }
    }

    function showMessageStatus(message, isError = false) {
        messageStatus.textContent = message;
        messageStatus.className = `text-sm ${isError ? 'text-red-500' : 'text-green-500'}`;
        setTimeout(() => {
            messageStatus.textContent = '';
        }, 5000);
    }

    async function handleLogout(deviceId) {
        if (!confirm('Are you sure you want to logout this device?')) {
            return;
        }

        try {
            const response = await fetch(`/logout/${deviceId}`, {
                method: 'POST'
            });
            const data = await response.json();
            
            if (data.error) {
                alert(data.error);
                return;
            }

            const deviceCard = deviceMap.get(deviceId);
            if (deviceCard) {
                deviceCard.remove();
                deviceMap.delete(deviceId);
            }

            if (currentDevice && currentDevice.id === deviceId) {
                showDevicesPage();
            }
        } catch (error) {
            console.error('Error logging out:', error);
            alert('Failed to logout. Please try again.');
        }
    }

    function showFunctionsPage(deviceInfo) {
        currentDevice = deviceInfo;
        document.querySelector('.container').classList.add('hidden');
        functionsPage.classList.remove('hidden');
        currentDeviceName.textContent = `${deviceInfo.name} (${deviceInfo.phoneNumber})`;
    }

    function showDevicesPage() {
        currentDevice = null;
        document.querySelector('.container').classList.remove('hidden');
        functionsPage.classList.add('hidden');
    }

    addDeviceBtn.addEventListener('click', async () => {
        try {
            qrSection.classList.remove('hidden');
            
            // Show loading state
            qrSection.innerHTML = `
                <div class="text-center p-4">
                    <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-gray-900 mx-auto"></div>
                    <p class="mt-2 text-gray-600">Generating QR code...</p>
                </div>
            `;
            
            const response = await fetch('/qr');
            const data = await response.json();
            
            if (data.error) {
                throw new Error(data.error);
            }

            // Check if data.qr.code exists
            if (!data.qr || !data.qr.code) {
                throw new Error('Invalid QR code data received');
            }

            console.log('Generating QR code with data:', data);
            
            // Reset qrSection content and create QR container
            qrSection.innerHTML = `
                <div class="flex flex-col items-center justify-center">
                    <div id="qr-container" class="bg-white p-4 rounded-lg shadow-md"></div>
                    <p class="mt-4 text-sm text-gray-600">Open WhatsApp on your phone and scan this QR code</p>
                </div>
            `;
            
            // Generate new QR code with the QR code string
            const qrContainer = document.getElementById('qr-container');
            const qr = new QRCode(qrContainer, {
                text: data.qr.code,
                width: 256,
                height: 256,
                colorDark: '#000000',
                colorLight: '#ffffff'
            });
            
            console.log('QR code generated successfully');
        } catch (error) {
            console.error('Error generating QR code:', error);
            
            // Show error message
            qrSection.innerHTML = `
                <div class="p-4 bg-red-50 border border-red-200 rounded-md">
                    <p class="text-red-600">${error.message}</p>
                    <p class="text-sm text-red-500 mt-2">Please try again</p>
                </div>
            `;
        }
    });

    backToDevicesBtn.addEventListener('click', showDevicesPage);

    async function sendMessage() {
        const recipient = document.getElementById('message-recipient').value.trim();
        const message = document.getElementById('message-content').value.trim();
        const saveToDb = document.getElementById('save-message').checked;

        if (!currentDevice) {
            showError('No device selected');
            return;
        }

        if (!recipient || !message) {
            showError('Please enter both recipient and message');
            return;
        }

        try {
            const response = await fetch('/messages/text', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    deviceId: currentDevice.id,
                    to: recipient.includes('@') ? recipient : `${recipient}@s.whatsapp.net`,
                    message: message,
                    save: saveToDb
                })
            });

            const data = await response.json();
            if (response.ok) {
                showMessageStatus('Message sent successfully!');
                document.getElementById('message-content').value = '';
            } else {
                throw new Error(data.error || 'Failed to send message');
            }
        } catch (error) {
            showMessageStatus('Error sending message: ' + error.message, true);
        }
    }

    document.getElementById('send-message-btn')?.addEventListener('click', sendMessage);

    fetchGroupsBtn.addEventListener('click', async () => {
        try {
            const shouldSave = document.getElementById('save-groups').checked;
            const endpoint = shouldSave ? `/get-groups/${currentDevice.id}?save=true` : `/get-groups/${currentDevice.id}`;
            
            // Show loading state
            groupsList.innerHTML = `
                <div class="text-sm text-gray-600">
                    <div class="animate-pulse flex space-x-2 items-center">
                        <div class="w-4 h-4 bg-blue-200 rounded-full"></div>
                        <div>Fetching groups... This may take a few minutes due to rate limiting.</div>
                    </div>
                    <div class="text-xs text-gray-500 mt-2">Please keep this page open while we fetch the data.</div>
                </div>
            `;
            
            const response = await fetch(endpoint);
            const data = await response.json();
            
            if (response.ok) {
                groupsList.innerHTML = '';
                
                if (data.groups.length === 0) {
                    groupsList.innerHTML = '<div class="text-sm text-gray-600">No groups found.</div>';
                    return;
                }

                data.groups.forEach(group => {
                    const groupElement = document.createElement('div');
                    groupElement.className = 'p-2 border rounded';
                    groupElement.innerHTML = `
                        <div class="font-medium">${group.name}</div>
                        <div class="text-sm text-gray-600">${group.participants} participants</div>
                    `;
                    groupsList.appendChild(groupElement);
                });

                if (shouldSave && data.saved) {
                    const saveStatus = document.createElement('div');
                    saveStatus.className = 'mt-4 p-3 bg-green-50 border border-green-200 rounded';
                    saveStatus.innerHTML = `
                        <div class="text-sm text-green-600 font-medium">Groups data saved successfully!</div>
                        <div class="text-xs text-green-500">
                            Total groups: ${data.saved.total}<br>
                            Last updated: ${new Date(data.saved.timestamp).toLocaleString()}
                        </div>
                    `;
                    groupsList.appendChild(saveStatus);
                    
                    // Remove the success message after 5 seconds
                    setTimeout(() => {
                        saveStatus.remove();
                    }, 5000);
                }
            } else {
                throw new Error(data.error || 'Failed to fetch groups');
            }
        } catch (error) {
            groupsList.innerHTML = `
                <div class="p-3 bg-red-50 border border-red-200 rounded">
                    <div class="text-sm text-red-600">Error fetching groups: ${error.message}</div>
                    <div class="text-xs text-red-500 mt-1">Please try again in a few minutes.</div>
                </div>
            `;
        }
    });

    // QR Code Modal Functions
    window.showQRModal = async () => {
        const modal = document.getElementById('qrModal');
        const qrDisplay = document.getElementById('qr-display');
        const qrStatus = document.getElementById('qr-status');
        
        if (!modal || !qrDisplay || !qrStatus) return;
        
        modal.classList.remove('hidden');
        qrStatus.textContent = 'Generating QR code...';
        qrDisplay.innerHTML = ''; // Clear any existing QR code
        
        try {
            const response = await fetch('/qr');
            const data = await response.json();
            
            if (data.error) {
                qrStatus.textContent = data.error;
                return;
            }

            if (!data.qr || !data.qr.code) {
                throw new Error('Invalid QR code data received');
            }

            // Generate new QR code with the QR code string
            const qr = new QRCode(qrDisplay, {
                text: data.qr.code,
                width: 256,
                height: 256,
                colorDark: '#000000',
                colorLight: '#ffffff'
            });
            
            qrStatus.textContent = 'Open WhatsApp on your phone and scan this QR code';
        } catch (error) {
            console.error('Error generating QR code:', error);
            qrStatus.textContent = `Error: ${error.message}`;
            qrDisplay.innerHTML = `
                <div class="p-4 bg-red-50 border border-red-200 rounded-md">
                    <p class="text-red-600">Failed to generate QR code</p>
                    <p class="text-sm text-red-500 mt-2">Please try again</p>
                </div>
            `;
        }
    };

    window.hideQRModal = () => {
        const modal = document.getElementById('qrModal');
        const qrDisplay = document.getElementById('qr-display');
        if (modal) {
            modal.classList.add('hidden');
            if (qrDisplay) {
                qrDisplay.innerHTML = ''; // Clear QR code when hiding modal
            }
        }
    };

    // API Testing Functions
    window.sendTextMessage = async () => {
        const deviceId = document.getElementById('deviceSelect').value;
        const recipient = document.getElementById('textMessageRecipient').value;
        const message = document.getElementById('textMessageContent').value;
        const response = document.getElementById('textMessageResponse');

        if (!deviceId) {
            showResponse(response, 'Please select a device', true);
            return;
        }

        if (!recipient || !message) {
            showResponse(response, 'Please fill in all fields', true);
            return;
        }

        try {
            const res = await fetch('/messages/text', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    deviceId,
                    to: recipient + '@s.whatsapp.net',
                    message
                })
            });

            const data = await res.json();
            showResponse(response, res.ok ? 'Message sent successfully' : data.error, !res.ok);
        } catch (error) {
            showResponse(response, 'Error sending message: ' + error.message, true);
        }
    };

    window.sendMediaMessage = async () => {
        const deviceId = document.getElementById('deviceSelect').value;
        const recipient = document.getElementById('mediaMessageRecipient').value;
        const caption = document.getElementById('mediaMessageCaption').value;
        const mediaType = document.getElementById('mediaType').value;
        const fileInput = document.getElementById('mediaFile');
        const response = document.getElementById('mediaMessageResponse');

        if (!deviceId) {
            showResponse(response, 'Please select a device', true);
            return;
        }

        if (!recipient || !fileInput.files[0]) {
            showResponse(response, 'Please fill in all required fields', true);
            return;
        }

        try {
            const file = fileInput.files[0];
            const reader = new FileReader();
            
            reader.onload = async (e) => {
                const base64Data = e.target.result.split(',')[1];
                
                const res = await fetch(`/messages/${mediaType}`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        deviceId,
                        to: recipient + '@s.whatsapp.net',
                        caption,
                        file: base64Data
                    })
                });

                const data = await res.json();
                showResponse(response, res.ok ? 'Media sent successfully' : data.error, !res.ok);
            };

            reader.readAsDataURL(file);
        } catch (error) {
            showResponse(response, 'Error sending media: ' + error.message, true);
        }
    };

    window.createGroup = async () => {
        const deviceId = document.getElementById('deviceSelect').value;
        const name = document.getElementById('groupName').value;
        const participants = document.getElementById('groupParticipants').value;
        const response = document.getElementById('createGroupResponse');

        if (!deviceId) {
            showResponse(response, 'Please select a device', true);
            return;
        }

        if (!name || !participants) {
            showResponse(response, 'Please fill in all fields', true);
            return;
        }

        try {
            const res = await fetch('/groups', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    deviceId,
                    name,
                    participants: participants.split(',').map(p => p.trim() + '@s.whatsapp.net')
                })
            });

            const data = await res.json();
            showResponse(response, res.ok ? 'Group created successfully' : data.error, !res.ok);
        } catch (error) {
            showResponse(response, 'Error creating group: ' + error.message, true);
        }
    };

    window.listGroups = async () => {
        const deviceId = document.getElementById('deviceSelect').value;
        const response = document.getElementById('listGroupsResponse');

        if (!deviceId) {
            showResponse(response, 'Please select a device', true);
            return;
        }

        try {
            const res = await fetch(`/groups?deviceId=${deviceId}`);
            const data = await res.json();

            if (res.ok) {
                response.innerHTML = '';
                data.groups.forEach(group => {
                    const groupDiv = document.createElement('div');
                    groupDiv.className = 'p-3 bg-gray-50 rounded-lg';
                    groupDiv.innerHTML = `
                        <div class="font-medium">${group.name}</div>
                        <div class="text-sm text-gray-600">${group.participants} participants</div>
                    `;
                    response.appendChild(groupDiv);
                });
            } else {
                showResponse(response, data.error, true);
            }
        } catch (error) {
            showResponse(response, 'Error fetching groups: ' + error.message, true);
        }
    };

    // Add new chat functionality
    document.getElementById('fetch-chats-btn')?.addEventListener('click', async () => {
        if (!currentDevice) {
            showError('No device selected');
            return;
        }

        try {
            const chatsList = document.getElementById('chats-list');
            chatsList.innerHTML = `
                <div class="animate-pulse flex space-x-2 items-center">
                    <div class="w-4 h-4 bg-blue-200 rounded-full"></div>
                    <div class="text-sm text-gray-600">Fetching chats...</div>
                </div>
            `;

            const response = await fetch(`/chats?deviceId=${currentDevice.id}`);
            const data = await response.json();

            if (!response.ok) {
                throw new Error(data.error || 'Failed to fetch chats');
            }

            chatsList.innerHTML = '';
            if (!data.data?.chats?.length) {
                chatsList.innerHTML = '<div class="text-sm text-gray-600">No chats found</div>';
                return;
            }

            // Sort chats by timestamp
            const chats = data.data.chats.sort((a, b) => {
                if (!a.timestamp) return 1;
                if (!b.timestamp) return -1;
                return new Date(b.timestamp) - new Date(a.timestamp);
            });

            chats.forEach(chat => {
                const chatElement = document.createElement('div');
                chatElement.className = 'bg-gray-50 rounded-lg p-4 space-y-2';
                
                const timestamp = chat.timestamp ? new Date(chat.timestamp).toLocaleString() : 'No messages';
                const statusIcons = [
                    chat.isArchived ? '📁' : '',
                    chat.isMuted ? '🔇' : '',
                    chat.isPinned ? '📌' : ''
                ].filter(Boolean).join(' ');

                chatElement.innerHTML = `
                    <div class="flex justify-between items-start">
                        <div>
                            <h4 class="font-medium">${chat.name}</h4>
                            <p class="text-sm text-gray-600">${chat.lastMessage || 'No messages'}</p>
                        </div>
                        <div class="text-right">
                            <div class="text-xs text-gray-500">${timestamp}</div>
                            <div class="flex space-x-1 mt-1">
                                ${statusIcons}
                                ${chat.unreadCount ? `<span class="bg-green-500 text-white text-xs px-2 py-0.5 rounded-full">${chat.unreadCount}</span>` : ''}
                            </div>
                        </div>
                    </div>
                    <div class="flex justify-between items-center mt-2">
                        <span class="text-xs text-gray-500">${chat.isGroup ? 'Group' : 'Private Chat'}</span>
                        <div class="space-x-2">
                            <button onclick="handleArchiveChat('${chat.id}', ${!chat.isArchived})" class="text-sm text-blue-500 hover:text-blue-700">
                                ${chat.isArchived ? 'Unarchive' : 'Archive'}
                            </button>
                            <button onclick="handleDeleteChat('${chat.id}')" class="text-sm text-red-500 hover:text-red-700">
                                Delete
                            </button>
                        </div>
                    </div>
                `;
                chatsList.appendChild(chatElement);
            });
        } catch (error) {
            console.error('Error fetching chats:', error);
            chatsList.innerHTML = `
                <div class="text-sm text-red-500">
                    Error fetching chats: ${error.message}
                </div>
            `;
        }
    });

    // Add chat management functions
    async function handleArchiveChat(chatId, archive) {
        if (!currentDevice) {
            showError('No device selected');
            return;
        }

        try {
            const response = await fetch(`/chats/${chatId}/archive?deviceId=${currentDevice.id}`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ archive })
            });

            const data = await response.json();
            if (response.ok) {
                // Refresh chats list
                document.getElementById('fetch-chats-btn').click();
            } else {
                throw new Error(data.error || 'Failed to archive chat');
            }
        } catch (error) {
            showError('Error archiving chat: ' + error.message);
        }
    }

    async function handleDeleteChat(chatId) {
        if (!confirm('Are you sure you want to delete this chat?')) {
            return;
        }

        if (!currentDevice) {
            showError('No device selected');
            return;
        }

        try {
            const response = await fetch(`/chats/${chatId}?deviceId=${currentDevice.id}`, {
                method: 'DELETE'
            });

            const data = await response.json();
            if (response.ok) {
                // Refresh chats list
                document.getElementById('fetch-chats-btn').click();
            } else {
                throw new Error(data.error || 'Failed to delete chat');
            }
        } catch (error) {
            showError('Error deleting chat: ' + error.message);
        }
    }

    // Helper Functions
    function showResponse(element, message, isError = false) {
        element.innerHTML = `
            <div class="p-3 rounded-md ${isError ? 'bg-red-50 text-red-700' : 'bg-green-50 text-green-700'}">
                ${message}
            </div>
        `;
    }

    function showError(message) {
        const errorDiv = document.createElement('div');
        errorDiv.className = 'fixed bottom-4 right-4 bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded';
        errorDiv.textContent = message;
        document.body.appendChild(errorDiv);
        setTimeout(() => errorDiv.remove(), 5000);
    }

    // Initialize
    loadDevices();
}); 