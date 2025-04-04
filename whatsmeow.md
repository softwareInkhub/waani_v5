Functions ¶
func DecryptMediaRetryNotification ¶
func DecryptMediaRetryNotification(evt *events.MediaRetry, mediaKey []byte) (*waMmsRetry.MediaRetryNotification, error)
DecryptMediaRetryNotification decrypts a media retry notification using the media key. See Client.SendMediaRetryReceipt for more info on how to use this.

func GenerateFacebookMessageID ¶
func GenerateFacebookMessageID() int64
func
GenerateMessageID
deprecated
func GetLatestVersion ¶
func GetLatestVersion(httpClient *http.Client) (*store.WAVersionContainer, error)
GetLatestVersion returns the latest version number from web.whatsapp.com.

After fetching, you can update the version to use using store.SetWAVersion, e.g.

latestVer, err := GetLatestVersion(nil)
if err != nil {
	return err
}
store.SetWAVersion(*latestVer)
func HashPollOptions ¶
func HashPollOptions(optionNames []string) [][]byte
HashPollOptions hashes poll option names using SHA-256 for voting. This is used by BuildPollVote to convert selected option names to hashes.

func ParseDisappearingTimerString ¶
func ParseDisappearingTimerString(val string) (time.Duration, bool)
ParseDisappearingTimerString parses common human-readable disappearing message timer strings into Duration values. If the string doesn't look like one of the allowed values (0, 24h, 7d, 90d), the second return value is false.

Types ¶
type APNsPushConfig ¶
type APNsPushConfig struct {
	Token       string `json:"token"`
	VoIPToken   string `json:"voip_token"`
	MsgIDEncKey []byte `json:"msg_id_enc_key"`
}
func (*APNsPushConfig) GetPushConfigAttrs ¶
func (apc *APNsPushConfig) GetPushConfigAttrs() waBinary.Attrs
type Client ¶
type Client struct {
	Store *store.Device
	Log   waLog.Logger

	EnableAutoReconnect   bool
	LastSuccessfulConnect time.Time
	AutoReconnectErrors   int
	// AutoReconnectHook is called when auto-reconnection fails. If the function returns false,
	// the client will not attempt to reconnect. The number of retries can be read from AutoReconnectErrors.
	AutoReconnectHook func(error) bool
	// If SynchronousAck is set, acks for messages will only be sent after all event handlers return.
	SynchronousAck bool

	DisableLoginAutoReconnect bool

	// EmitAppStateEventsOnFullSync can be set to true if you want to get app state events emitted
	// even when re-syncing the whole state.
	EmitAppStateEventsOnFullSync bool

	AutomaticMessageRerequestFromPhone bool

	// GetMessageForRetry is used to find the source message for handling retry receipts
	// when the message is not found in the recently sent message cache.
	GetMessageForRetry func(requester, to types.JID, id types.MessageID) *waE2E.Message
	// PreRetryCallback is called before a retry receipt is accepted.
	// If it returns false, the accepting will be cancelled and the retry receipt will be ignored.
	PreRetryCallback func(receipt *events.Receipt, id types.MessageID, retryCount int, msg *waE2E.Message) bool

	// PrePairCallback is called before pairing is completed. If it returns false, the pairing will be cancelled and
	// the client will disconnect.
	PrePairCallback func(jid types.JID, platform, businessName string) bool

	// GetClientPayload is called to get the client payload for connecting to the server.
	// This should NOT be used for WhatsApp (to change the OS name, update fields in store.BaseClientPayload directly).
	GetClientPayload func() *waWa6.ClientPayload

	// Should untrusted identity errors be handled automatically? If true, the stored identity and existing signal
	// sessions will be removed on untrusted identity errors, and an events.IdentityChange will be dispatched.
	// If false, decrypting a message from untrusted devices will fail.
	AutoTrustIdentity bool

	// Should SubscribePresence return an error if no privacy token is stored for the user?
	ErrorOnSubscribePresenceWithoutToken bool

	// This field changes the client to act like a Messenger client instead of a WhatsApp one.
	//
	// Note that you cannot use a Messenger account just by setting this field, you must use a
	// separate library for all the non-e2ee-related stuff like logging in.
	// The library is currently embedded in mautrix-meta (https://github.com/mautrix/meta), but may be separated later.
	MessengerConfig *MessengerConfig
	RefreshCAT      func() error
	// contains filtered or unexported fields
}
Client contains everything necessary to connect to and interact with the WhatsApp web API.

func NewClient ¶
func NewClient(deviceStore *store.Device, log waLog.Logger) *Client
NewClient initializes a new WhatsApp web client.

The logger can be nil, it will default to a no-op logger.

The device store must be set. A default SQL-backed implementation is available in the store/sqlstore package.

container, err := sqlstore.New("sqlite3", "file:yoursqlitefile.db?_foreign_keys=on", nil)
if err != nil {
	panic(err)
}
// If you want multiple sessions, remember their JIDs and use .GetDevice(jid) or .GetAllDevices() instead.
deviceStore, err := container.GetFirstDevice()
if err != nil {
	panic(err)
}
client := whatsmeow.NewClient(deviceStore, nil)
func (*Client) AcceptTOSNotice ¶
func (cli *Client) AcceptTOSNotice(noticeID, stage string) error
AcceptTOSNotice accepts a ToS notice.

To accept the terms for creating newsletters, use

cli.AcceptTOSNotice("20601218", "5")
func (*Client) AddEventHandler ¶
func (cli *Client) AddEventHandler(handler EventHandler) uint32
AddEventHandler registers a new function to receive all events emitted by this client.

The returned integer is the event handler ID, which can be passed to RemoveEventHandler to remove it.

All registered event handlers will receive all events. You should use a type switch statement to filter the events you want:

func myEventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Println("Received a message!")
	case *events.Receipt:
		fmt.Println("Received a receipt!")
	}
}
If you want to access the Client instance inside the event handler, the recommended way is to wrap the whole handler in another struct:

type MyClient struct {
	WAClient *whatsmeow.Client
	eventHandlerID uint32
}

func (mycli *MyClient) register() {
	mycli.eventHandlerID = mycli.WAClient.AddEventHandler(mycli.myEventHandler)
}

func (mycli *MyClient) myEventHandler(evt interface{}) {
	// Handle event and access mycli.WAClient
}
func (*Client) BuildEdit ¶
func (cli *Client) BuildEdit(chat types.JID, id types.MessageID, newContent *waE2E.Message) *waE2E.Message
BuildEdit builds a message edit message using the given variables. The built message can be sent normally using Client.SendMessage.

resp, err := cli.SendMessage(context.Background(), chat, cli.BuildEdit(chat, originalMessageID, &waE2E.Message{
	Conversation: proto.String("edited message"),
})
func (*Client) BuildHistorySyncRequest ¶
func (cli *Client) BuildHistorySyncRequest(lastKnownMessageInfo *types.MessageInfo, count int) *waE2E.Message
BuildHistorySyncRequest builds a message to request additional history from the user's primary device.

The built message can be sent using Client.SendMessage, but you must pass whatsmeow.SendRequestExtra{Peer: true} as the last parameter. The response will come as an *events.HistorySync with type `ON_DEMAND`.

The response will contain to `count` messages immediately before the given message. The recommended number of messages to request at a time is 50.

func (*Client) BuildMessageKey ¶
func (cli *Client) BuildMessageKey(chat, sender types.JID, id types.MessageID) *waCommon.MessageKey
BuildMessageKey builds a MessageKey object, which is used to refer to previous messages for things such as replies, revocations and reactions.

func (*Client) BuildPollCreation ¶
func (cli *Client) BuildPollCreation(name string, optionNames []string, selectableOptionCount int) *waE2E.Message
BuildPollCreation builds a poll creation message with the given poll name, options and maximum number of selections. The built message can be sent normally using Client.SendMessage.

resp, err := cli.SendMessage(context.Background(), chat, cli.BuildPollCreation("meow?", []string{"yes", "no"}, 1))
func (*Client) BuildPollVote ¶
func (cli *Client) BuildPollVote(pollInfo *types.MessageInfo, optionNames []string) (*waE2E.Message, error)
BuildPollVote builds a poll vote message using the given poll message info and option names. The built message can be sent normally using Client.SendMessage.

For example, to vote for the first option after receiving a message event (*events.Message):

if evt.Message.GetPollCreationMessage() != nil {
	pollVoteMsg, err := cli.BuildPollVote(&evt.Info, []string{evt.Message.GetPollCreationMessage().GetOptions()[0].GetOptionName()})
	if err != nil {
		fmt.Println(":(", err)
		return
	}
	resp, err := cli.SendMessage(context.Background(), evt.Info.Chat, pollVoteMsg)
}
func (*Client) BuildReaction ¶
func (cli *Client) BuildReaction(chat, sender types.JID, id types.MessageID, reaction string) *waE2E.Message
BuildReaction builds a message reaction message using the given variables. The built message can be sent normally using Client.SendMessage.

resp, err := cli.SendMessage(context.Background(), chat, cli.BuildReaction(chat, senderJID, targetMessageID, "🐈️")
Note that for newsletter messages, you need to use NewsletterSendReaction instead of BuildReaction + SendMessage.

func (*Client) BuildRevoke ¶
func (cli *Client) BuildRevoke(chat, sender types.JID, id types.MessageID) *waE2E.Message
BuildRevoke builds a message revocation message using the given variables. The built message can be sent normally using Client.SendMessage.

To revoke your own messages, pass your JID or an empty JID as the second parameter (sender).

resp, err := cli.SendMessage(context.Background(), chat, cli.BuildRevoke(chat, types.EmptyJID, originalMessageID)
To revoke someone else's messages when you are group admin, pass the message sender's JID as the second parameter.

resp, err := cli.SendMessage(context.Background(), chat, cli.BuildRevoke(chat, senderJID, originalMessageID)
func (*Client) BuildUnavailableMessageRequest ¶
func (cli *Client) BuildUnavailableMessageRequest(chat, sender types.JID, id string) *waE2E.Message
BuildUnavailableMessageRequest builds a message to request the user's primary device to send the copy of a message that this client was unable to decrypt.

The built message can be sent using Client.SendMessage, but you must pass whatsmeow.SendRequestExtra{Peer: true} as the last parameter. The full response will come as a ProtocolMessage with type `PEER_DATA_OPERATION_REQUEST_RESPONSE_MESSAGE`. The response events will also be dispatched as normal *events.Message's with UnavailableRequestID set to the request message ID.

func (*Client) Connect ¶
func (cli *Client) Connect() error
Connect connects the client to the WhatsApp web websocket. After connection, it will either authenticate if there's data in the device store, or emit a QREvent to set up a new link.

func (*Client) CreateGroup ¶
func (cli *Client) CreateGroup(req ReqCreateGroup) (*types.GroupInfo, error)
CreateGroup creates a group on WhatsApp with the given name and participants.

See ReqCreateGroup for parameters.

func (*Client) CreateNewsletter ¶
func (cli *Client) CreateNewsletter(params CreateNewsletterParams) (*types.NewsletterMetadata, error)
CreateNewsletter creates a new WhatsApp channel.

func (*Client)
DangerousInternals
deprecated
func (*Client) DecryptPollVote ¶
func (cli *Client) DecryptPollVote(vote *events.Message) (*waE2E.PollVoteMessage, error)
DecryptPollVote decrypts a poll update message. The vote itself includes SHA-256 hashes of the selected options.

if evt.Message.GetPollUpdateMessage() != nil {
	pollVote, err := cli.DecryptPollVote(evt)
	if err != nil {
		fmt.Println(":(", err)
		return
	}
	fmt.Println("Selected hashes:")
	for _, hash := range pollVote.GetSelectedOptions() {
		fmt.Printf("- %X\n", hash)
	}
}
func (*Client) DecryptReaction ¶
func (cli *Client) DecryptReaction(reaction *events.Message) (*waE2E.ReactionMessage, error)
DecryptReaction decrypts a reaction update message. This form of reactions hasn't been rolled out yet, so this function is likely not of much use.

if evt.Message.GetEncReactionMessage() != nil {
	reaction, err := cli.DecryptReaction(evt)
	if err != nil {
		fmt.Println(":(", err)
		return
	}
	fmt.Printf("Reaction message: %+v\n", reaction)
}
func (*Client) Disconnect ¶
func (cli *Client) Disconnect()
Disconnect disconnects from the WhatsApp web websocket.

This will not emit any events, the Disconnected event is only used when the connection is closed by the server or a network error.

func (*Client) Download ¶
func (cli *Client) Download(msg DownloadableMessage) ([]byte, error)
Download downloads the attachment from the given protobuf message.

The attachment is a specific part of a Message protobuf struct, not the message itself, e.g.

var msg *waE2E.Message
...
imageData, err := cli.Download(msg.GetImageMessage())
You can also use DownloadAny to download the first non-nil sub-message.

func (*Client) DownloadAny ¶
func (cli *Client) DownloadAny(msg *waE2E.Message) (data []byte, err error)
DownloadAny loops through the downloadable parts of the given message and downloads the first non-nil item.

func (*Client) DownloadFB ¶
func (cli *Client) DownloadFB(transport *waMediaTransport.WAMediaTransport_Integral, mediaType MediaType) ([]byte, error)
func (*Client) DownloadFBToFile ¶
func (cli *Client) DownloadFBToFile(transport *waMediaTransport.WAMediaTransport_Integral, mediaType MediaType, file File) error
func (*Client) DownloadMediaWithPath ¶
func (cli *Client) DownloadMediaWithPath(directPath string, encFileHash, fileHash, mediaKey []byte, fileLength int, mediaType MediaType, mmsType string) (data []byte, err error)
DownloadMediaWithPath downloads an attachment by manually specifying the path and encryption details.

func (*Client) DownloadMediaWithPathToFile ¶
func (cli *Client) DownloadMediaWithPathToFile(directPath string, encFileHash, fileHash, mediaKey []byte, fileLength int, mediaType MediaType, mmsType string, file File) error
func (*Client) DownloadThumbnail ¶
func (cli *Client) DownloadThumbnail(msg DownloadableThumbnail) ([]byte, error)
DownloadThumbnail downloads a thumbnail from a message.

This is primarily intended for downloading link preview thumbnails, which are in ExtendedTextMessage:

var msg *waE2E.Message
...
thumbnailImageBytes, err := cli.DownloadThumbnail(msg.GetExtendedTextMessage())
func (*Client) DownloadToFile ¶
func (cli *Client) DownloadToFile(msg DownloadableMessage, file File) error
DownloadToFile downloads the attachment from the given protobuf message.

This is otherwise identical to [Download], but writes the attachment to a file instead of returning it as a byte slice.

func (*Client) EncryptPollVote ¶
func (cli *Client) EncryptPollVote(pollInfo *types.MessageInfo, vote *waE2E.PollVoteMessage) (*waE2E.PollUpdateMessage, error)
EncryptPollVote encrypts a poll vote message. This is a slightly lower-level function, using BuildPollVote is recommended.

func (*Client) FetchAppState ¶
func (cli *Client) FetchAppState(name appstate.WAPatchName, fullSync, onlyIfNotSynced bool) error
FetchAppState fetches updates to the given type of app state. If fullSync is true, the current cached state will be removed and all app state patches will be re-fetched from the server.

func (*Client) FollowNewsletter ¶
func (cli *Client) FollowNewsletter(jid types.JID) error
FollowNewsletter makes the user follow (join) a WhatsApp channel.

func (*Client) GenerateMessageID ¶
func (cli *Client) GenerateMessageID() types.MessageID
GenerateMessageID generates a random string that can be used as a message ID on WhatsApp.

msgID := cli.GenerateMessageID()
cli.SendMessage(context.Background(), targetJID, &waE2E.Message{...}, whatsmeow.SendRequestExtra{ID: msgID})
func (*Client) GetBlocklist ¶
func (cli *Client) GetBlocklist() (*types.Blocklist, error)
GetBlocklist gets the list of users that this user has blocked.

func (*Client) GetBotListV2 ¶
func (cli *Client) GetBotListV2() ([]types.BotListInfo, error)
func (*Client) GetBotProfiles ¶
func (cli *Client) GetBotProfiles(botInfo []types.BotListInfo) ([]types.BotProfileInfo, error)
func (*Client) GetBusinessProfile ¶
func (cli *Client) GetBusinessProfile(jid types.JID) (*types.BusinessProfile, error)
GetBusinessProfile gets the profile info of a WhatsApp business account

func (*Client) GetContactQRLink ¶
func (cli *Client) GetContactQRLink(revoke bool) (string, error)
GetContactQRLink gets your own contact share QR link that can be resolved using ResolveContactQRLink (or scanned with the official apps when encoded as a QR code).

If the revoke parameter is set to true, it will ask the server to revoke the previous link and generate a new one.

func (*Client) GetGroupInfo ¶
func (cli *Client) GetGroupInfo(jid types.JID) (*types.GroupInfo, error)
GetGroupInfo requests basic info about a group chat from the WhatsApp servers.

func (*Client) GetGroupInfoFromInvite ¶
func (cli *Client) GetGroupInfoFromInvite(jid, inviter types.JID, code string, expiration int64) (*types.GroupInfo, error)
GetGroupInfoFromInvite gets the group info from an invite message.

Note that this is specifically for invite messages, not invite links. Use GetGroupInfoFromLink for resolving chat.whatsapp.com links.

func (*Client) GetGroupInfoFromLink ¶
func (cli *Client) GetGroupInfoFromLink(code string) (*types.GroupInfo, error)
GetGroupInfoFromLink resolves the given invite link and asks the WhatsApp servers for info about the group. This will not cause the user to join the group.

func (*Client) GetGroupInviteLink ¶
func (cli *Client) GetGroupInviteLink(jid types.JID, reset bool) (string, error)
GetGroupInviteLink requests the invite link to the group from the WhatsApp servers.

If reset is true, then the old invite link will be revoked and a new one generated.

func (*Client) GetGroupRequestParticipants ¶
func (cli *Client) GetGroupRequestParticipants(jid types.JID) ([]types.GroupParticipantRequest, error)
GetGroupRequestParticipants gets the list of participants that have requested to join the group.

func (*Client) GetJoinedGroups ¶
func (cli *Client) GetJoinedGroups() ([]*types.GroupInfo, error)
GetJoinedGroups returns the list of groups the user is participating in.

func (*Client) GetLinkedGroupsParticipants ¶
func (cli *Client) GetLinkedGroupsParticipants(community types.JID) ([]types.JID, error)
GetLinkedGroupsParticipants gets all the participants in the groups of the given community.

func (*Client) GetNewsletterInfo ¶
func (cli *Client) GetNewsletterInfo(jid types.JID) (*types.NewsletterMetadata, error)
GetNewsletterInfo gets the info of a newsletter that you're joined to.

func (*Client) GetNewsletterInfoWithInvite ¶
func (cli *Client) GetNewsletterInfoWithInvite(key string) (*types.NewsletterMetadata, error)
GetNewsletterInfoWithInvite gets the info of a newsletter with an invite link.

You can either pass the full link (https://whatsapp.com/channel/...) or just the `...` part.

Note that the ViewerMeta field of the returned NewsletterMetadata will be nil.

func (*Client) GetNewsletterMessageUpdates ¶
func (cli *Client) GetNewsletterMessageUpdates(jid types.JID, params *GetNewsletterUpdatesParams) ([]*types.NewsletterMessage, error)
GetNewsletterMessageUpdates gets updates in a WhatsApp channel.

These are the same kind of updates that NewsletterSubscribeLiveUpdates triggers (reaction and view counts).

func (*Client) GetNewsletterMessages ¶
func (cli *Client) GetNewsletterMessages(jid types.JID, params *GetNewsletterMessagesParams) ([]*types.NewsletterMessage, error)
GetNewsletterMessages gets messages in a WhatsApp channel.

func (*Client) GetPrivacySettings ¶
func (cli *Client) GetPrivacySettings() (settings types.PrivacySettings)
GetPrivacySettings will get the user's privacy settings. If an error occurs while fetching them, the error will be logged, but the method will just return an empty struct.

func (*Client) GetProfilePictureInfo ¶
func (cli *Client) GetProfilePictureInfo(jid types.JID, params *GetProfilePictureParams) (*types.ProfilePictureInfo, error)
GetProfilePictureInfo gets the URL where you can download a WhatsApp user's profile picture or group's photo.

Optionally, you can pass the last known profile picture ID. If the profile picture hasn't changed, this will return nil with no error.

To get a community photo, you should pass `IsCommunity: true`, as otherwise you may get a 401 error.

func (*Client) GetQRChannel ¶
func (cli *Client) GetQRChannel(ctx context.Context) (<-chan QRChannelItem, error)
GetQRChannel returns a channel that automatically outputs a new QR code when the previous one expires.

This must be called *before* Connect(). It will then listen to all the relevant events from the client.

The last value to be emitted will be a special event like "success", "timeout" or another error code depending on the result of the pairing. The channel will be closed immediately after one of those.

func (*Client) GetServerPushNotificationConfig ¶
func (cli *Client) GetServerPushNotificationConfig(ctx context.Context) (*waBinary.Node, error)
func (*Client) GetStatusPrivacy ¶
func (cli *Client) GetStatusPrivacy() ([]types.StatusPrivacy, error)
GetStatusPrivacy gets the user's status privacy settings (who to send status broadcasts to).

There can be multiple different stored settings, the first one is always the default.

func (*Client) GetSubGroups ¶
func (cli *Client) GetSubGroups(community types.JID) ([]*types.GroupLinkTarget, error)
GetSubGroups gets the subgroups of the given community.

func (*Client) GetSubscribedNewsletters ¶
func (cli *Client) GetSubscribedNewsletters() ([]*types.NewsletterMetadata, error)
GetSubscribedNewsletters gets the info of all newsletters that you're joined to.

func (*Client) GetUserDevices ¶
func (cli *Client) GetUserDevices(jids []types.JID) ([]types.JID, error)
GetUserDevices gets the list of devices that the given user has. The input should be a list of regular JIDs, and the output will be a list of AD JIDs. The local device will not be included in the output even if the user's JID is included in the input. All other devices will be included.

func (*Client) GetUserDevicesContext ¶
func (cli *Client) GetUserDevicesContext(ctx context.Context, jids []types.JID) ([]types.JID, error)
func (*Client) GetUserInfo ¶
func (cli *Client) GetUserInfo(jids []types.JID) (map[types.JID]types.UserInfo, error)
GetUserInfo gets basic user info (avatar, status, verified business name, device list).

func (*Client) IsConnected ¶
func (cli *Client) IsConnected() bool
IsConnected checks if the client is connected to the WhatsApp web websocket. Note that this doesn't check if the client is authenticated. See the IsLoggedIn field for that.

func (*Client) IsLoggedIn ¶
func (cli *Client) IsLoggedIn() bool
IsLoggedIn returns true after the client is successfully connected and authenticated on WhatsApp.

func (*Client) IsOnWhatsApp ¶
func (cli *Client) IsOnWhatsApp(phones []string) ([]types.IsOnWhatsAppResponse, error)
IsOnWhatsApp checks if the given phone numbers are registered on WhatsApp. The phone numbers should be in international format, including the `+` prefix.

func (*Client) JoinGroupWithInvite ¶
func (cli *Client) JoinGroupWithInvite(jid, inviter types.JID, code string, expiration int64) error
JoinGroupWithInvite joins a group using an invite message.

Note that this is specifically for invite messages, not invite links. Use JoinGroupWithLink for joining with chat.whatsapp.com links.

func (*Client) JoinGroupWithLink ¶
func (cli *Client) JoinGroupWithLink(code string) (types.JID, error)
JoinGroupWithLink joins the group using the given invite link.

func (*Client) LeaveGroup ¶
func (cli *Client) LeaveGroup(jid types.JID) error
LeaveGroup leaves the specified group on WhatsApp.

func (*Client) LinkGroup ¶
func (cli *Client) LinkGroup(parent, child types.JID) error
LinkGroup adds an existing group as a child group in a community.

To create a new group within a community, set LinkedParentJID in the CreateGroup request.

func (*Client) Logout ¶
func (cli *Client) Logout() error
Logout sends a request to unlink the device, then disconnects from the websocket and deletes the local device store.

If the logout request fails, the disconnection and local data deletion will not happen either. If an error is returned, but you want to force disconnect/clear data, call Client.Disconnect() and Client.Store.Delete() manually.

Note that this will not emit any events. The LoggedOut event is only used for external logouts (triggered by the user from the main device or by WhatsApp servers).

func (*Client) MarkRead ¶
func (cli *Client) MarkRead(ids []types.MessageID, timestamp time.Time, chat, sender types.JID, receiptTypeExtra ...types.ReceiptType) error
MarkRead sends a read receipt for the given message IDs including the given timestamp as the read at time.

The first JID parameter (chat) must always be set to the chat ID (user ID in DMs and group ID in group chats). The second JID parameter (sender) must be set in group chats and must be the user ID who sent the message.

You can mark multiple messages as read at the same time, but only if the messages were sent by the same user. To mark messages by different users as read, you must call MarkRead multiple times (once for each user).

To mark a voice message as played, specify types.ReceiptTypePlayed as the last parameter. Providing more than one receipt type will panic: the parameter is only a vararg for backwards compatibility.

func (*Client) NewsletterMarkViewed ¶
func (cli *Client) NewsletterMarkViewed(jid types.JID, serverIDs []types.MessageServerID) error
NewsletterMarkViewed marks a channel message as viewed, incrementing the view counter.

This is not the same as marking the channel as read on your other devices, use the usual MarkRead function for that.

func (*Client) NewsletterSendReaction ¶
func (cli *Client) NewsletterSendReaction(jid types.JID, serverID types.MessageServerID, reaction string, messageID types.MessageID) error
NewsletterSendReaction sends a reaction to a channel message. To remove a reaction sent earlier, set reaction to an empty string.

The last parameter is the message ID of the reaction itself. It can be left empty to let whatsmeow generate a random one.

func (*Client) NewsletterSubscribeLiveUpdates ¶
func (cli *Client) NewsletterSubscribeLiveUpdates(ctx context.Context, jid types.JID) (time.Duration, error)
NewsletterSubscribeLiveUpdates subscribes to receive live updates from a WhatsApp channel temporarily (for the duration returned).

func (*Client) NewsletterToggleMute ¶
func (cli *Client) NewsletterToggleMute(jid types.JID, mute bool) error
NewsletterToggleMute changes the mute status of a newsletter.

func (*Client) PairPhone ¶
func (cli *Client) PairPhone(phone string, showPushNotification bool, clientType PairClientType, clientDisplayName string) (string, error)
PairPhone generates a pairing code that can be used to link to a phone without scanning a QR code.

You must connect the client normally before calling this (which means you'll also receive a QR code event, but that can be ignored when doing code pairing). You should also wait for `*events.QR` before calling this to ensure the connection is fully established. If using Client.GetQRChannel, wait for the first item in the channel. Alternatively, sleeping for a second after calling Connect will probably work too.

The exact expiry of pairing codes is unknown, but QR codes are always generated and the login websocket is closed after the QR codes run out, which means there's a 160-second time limit. It is recommended to generate the pairing code immediately after connecting to the websocket to have the maximum time.

The clientType parameter must be one of the PairClient* constants, but which one doesn't matter. The client display name must be formatted as `Browser (OS)`, and only common browsers/OSes are allowed (the server will validate it and return 400 if it's wrong).

See https://faq.whatsapp.com/1324084875126592 for more info

func (*Client) ParseWebMessage ¶
func (cli *Client) ParseWebMessage(chatJID types.JID, webMsg *waWeb.WebMessageInfo) (*events.Message, error)
ParseWebMessage parses a WebMessageInfo object into *events.Message to match what real-time messages have.

The chat JID can be found in the Conversation data:

chatJID, err := types.ParseJID(conv.GetId())
for _, historyMsg := range conv.GetMessages() {
	evt, err := cli.ParseWebMessage(chatJID, historyMsg.GetMessage())
	yourNormalEventHandler(evt)
}
func (*Client) RegisterForPushNotifications ¶
func (cli *Client) RegisterForPushNotifications(ctx context.Context, pc PushConfig) error
RegisterForPushNotifications registers a token to receive push notifications for new WhatsApp messages.

This is generally not necessary for anything. Don't use this if you don't know what you're doing.

func (*Client) RejectCall ¶
func (cli *Client) RejectCall(callFrom types.JID, callID string) error
RejectCall reject an incoming call.

func (*Client) RemoveEventHandler ¶
func (cli *Client) RemoveEventHandler(id uint32) bool
RemoveEventHandler removes a previously registered event handler function. If the function with the given ID is found, this returns true.

N.B. Do not run this directly from an event handler. That would cause a deadlock because the event dispatcher holds a read lock on the event handler list, and this method wants a write lock on the same list. Instead run it in a goroutine:

func (mycli *MyClient) myEventHandler(evt interface{}) {
	if noLongerWantEvents {
		go mycli.WAClient.RemoveEventHandler(mycli.eventHandlerID)
	}
}
func (*Client) RemoveEventHandlers ¶
func (cli *Client) RemoveEventHandlers()
RemoveEventHandlers removes all event handlers that have been registered with AddEventHandler

func (*Client) ResolveBusinessMessageLink ¶
func (cli *Client) ResolveBusinessMessageLink(code string) (*types.BusinessMessageLinkTarget, error)
ResolveBusinessMessageLink resolves a business message short link and returns the target JID, business name and text to prefill in the input field (if any).

The links look like https://wa.me/message/<code> or https://api.whatsapp.com/message/<code>. You can either provide the full link, or just the <code> part.

func (*Client) ResolveContactQRLink ¶
func (cli *Client) ResolveContactQRLink(code string) (*types.ContactQRLinkTarget, error)
ResolveContactQRLink resolves a link from a contact share QR code and returns the target JID and push name.

The links look like https://wa.me/qr/<code> or https://api.whatsapp.com/qr/<code>. You can either provide the full link, or just the <code> part.

func (*Client)
RevokeMessage
deprecated
func (*Client) SendAppState ¶
func (cli *Client) SendAppState(patch appstate.PatchInfo) error
SendAppState sends the given app state patch, then resyncs that app state type from the server to update local caches and send events for the updates.

You can use the Build methods in the appstate package to build the parameter for this method, e.g.

cli.SendAppState(appstate.BuildMute(targetJID, true, 24 * time.Hour))
func (*Client) SendChatPresence ¶
func (cli *Client) SendChatPresence(jid types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error
SendChatPresence updates the user's typing status in a specific chat.

The media parameter can be set to indicate the user is recording media (like a voice message) rather than typing a text message.

func (*Client) SendFBMessage ¶
func (cli *Client) SendFBMessage(
	ctx context.Context,
	to types.JID,
	message armadillo.RealMessageApplicationSub,
	metadata *waMsgApplication.MessageApplication_Metadata,
	extra ...SendRequestExtra,
) (resp SendResponse, err error)
SendFBMessage sends the given v3 message to the given JID.

func (*Client) SendMediaRetryReceipt ¶
func (cli *Client) SendMediaRetryReceipt(message *types.MessageInfo, mediaKey []byte) error
SendMediaRetryReceipt sends a request to the phone to re-upload the media in a message.

This is mostly relevant when handling history syncs and getting a 404 or 410 error downloading media. Rough example on how to use it (will not work out of the box, you must adjust it depending on what you need exactly):

var mediaRetryCache map[types.MessageID]*waE2E.ImageMessage

evt, err := cli.ParseWebMessage(chatJID, historyMsg.GetMessage())
imageMsg := evt.Message.GetImageMessage() // replace this with the part of the message you want to download
data, err := cli.Download(imageMsg)
if errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) || errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) {
  err = cli.SendMediaRetryReceipt(&evt.Info, imageMsg.GetMediaKey())
  // You need to store the event data somewhere as it's necessary for handling the retry response.
  mediaRetryCache[evt.Info.ID] = imageMsg
}
The response will come as an *events.MediaRetry. The response will then have to be decrypted using DecryptMediaRetryNotification and the same media key passed here. If the media retry was successful, the decrypted notification should contain an updated DirectPath, which can be used to download the file.

func eventHandler(rawEvt interface{}) {
  switch evt := rawEvt.(type) {
  case *events.MediaRetry:
    imageMsg := mediaRetryCache[evt.MessageID]
    retryData, err := whatsmeow.DecryptMediaRetryNotification(evt, imageMsg.GetMediaKey())
    if err != nil || retryData.GetResult != waMmsRetry.MediaRetryNotification_SUCCESS {
      return
    }
    // Use the new path to download the attachment
    imageMsg.DirectPath = retryData.DirectPath
    data, err := cli.Download(imageMsg)
    // Alternatively, you can use cli.DownloadMediaWithPath and provide the individual fields manually.
  }
}
func (*Client) SendMessage ¶
func (cli *Client) SendMessage(ctx context.Context, to types.JID, message *waE2E.Message, extra ...SendRequestExtra) (resp SendResponse, err error)
SendMessage sends the given message.

This method will wait for the server to acknowledge the message before returning. The return value is the timestamp of the message from the server.

Optional parameters like the message ID can be specified with the SendRequestExtra struct. Only one extra parameter is allowed, put all necessary parameters in the same struct.

The message itself can contain anything you want (within the protobuf schema). e.g. for a simple text message, use the Conversation field:

cli.SendMessage(context.Background(), targetJID, &waE2E.Message{
	Conversation: proto.String("Hello, World!"),
})
Things like replies, mentioning users and the "forwarded" flag are stored in ContextInfo, which can be put in ExtendedTextMessage and any of the media message types.

For uploading and sending media/attachments, see the Upload method.

For other message types, you'll have to figure it out yourself. Looking at the protobuf schema in binary/proto/def.proto may be useful to find out all the allowed fields. Printing the RawMessage field in incoming message events to figure out what it contains is also a good way to learn how to send the same kind of message.

func (*Client) SendPresence ¶
func (cli *Client) SendPresence(state types.Presence) error
SendPresence updates the user's presence status on WhatsApp.

You should call this at least once after connecting so that the server has your pushname. Otherwise, other users will see "-" as the name.

func (*Client) SetDefaultDisappearingTimer ¶
func (cli *Client) SetDefaultDisappearingTimer(timer time.Duration) (err error)
SetDefaultDisappearingTimer will set the default disappearing message timer.

func (*Client) SetDisappearingTimer ¶
func (cli *Client) SetDisappearingTimer(chat types.JID, timer time.Duration) (err error)
SetDisappearingTimer sets the disappearing timer in a chat. Both private chats and groups are supported, but they're set with different methods.

Note that while this function allows passing non-standard durations, official WhatsApp apps will ignore those, and in groups the server will just reject the change. You can use the DisappearingTimer<Duration> constants for convenience.

In groups, the server will echo the change as a notification, so it'll show up as a *events.GroupInfo update.

func (*Client) SetForceActiveDeliveryReceipts ¶
func (cli *Client) SetForceActiveDeliveryReceipts(active bool)
SetForceActiveDeliveryReceipts will force the client to send normal delivery receipts (which will show up as the two gray ticks on WhatsApp), even if the client isn't marked as online.

By default, clients that haven't been marked as online will send delivery receipts with type="inactive", which is transmitted to the sender, but not rendered in the official WhatsApp apps. This is consistent with how WhatsApp web works when it's not in the foreground.

To mark the client as online, use

cli.SendPresence(types.PresenceAvailable)
Note that if you turn this off (i.e. call SetForceActiveDeliveryReceipts(false)), receipts will act like the client is offline until SendPresence is called again.

func (*Client) SetGroupAnnounce ¶
func (cli *Client) SetGroupAnnounce(jid types.JID, announce bool) error
SetGroupAnnounce changes whether the group is in announce mode (i.e. whether only admins can send messages).

func (*Client) SetGroupDescription ¶
func (cli *Client) SetGroupDescription(jid types.JID, description string) error
SetGroupDescription updates the group description.

func (*Client) SetGroupJoinApprovalMode ¶
func (cli *Client) SetGroupJoinApprovalMode(jid types.JID, mode bool) error
SetGroupJoinApprovalMode sets the group join approval mode to 'on' or 'off'.

func (*Client) SetGroupLocked ¶
func (cli *Client) SetGroupLocked(jid types.JID, locked bool) error
SetGroupLocked changes whether the group is locked (i.e. whether only admins can modify group info).

func (*Client) SetGroupMemberAddMode ¶
func (cli *Client) SetGroupMemberAddMode(jid types.JID, mode types.GroupMemberAddMode) error
SetGroupMemberAddMode sets the group member add mode to 'admin_add' or 'all_member_add'.

func (*Client) SetGroupName ¶
func (cli *Client) SetGroupName(jid types.JID, name string) error
SetGroupName updates the name (subject) of the given group on WhatsApp.

func (*Client) SetGroupPhoto ¶
func (cli *Client) SetGroupPhoto(jid types.JID, avatar []byte) (string, error)
SetGroupPhoto updates the group picture/icon of the given group on WhatsApp. The avatar should be a JPEG photo, other formats may be rejected with ErrInvalidImageFormat. The bytes can be nil to remove the photo. Returns the new picture ID.

func (*Client) SetGroupTopic ¶
func (cli *Client) SetGroupTopic(jid types.JID, previousID, newID, topic string) error
SetGroupTopic updates the topic (description) of the given group on WhatsApp.

The previousID and newID fields are optional. If the previous ID is not specified, this will automatically fetch the current group info to find the previous topic ID. If the new ID is not specified, one will be generated with Client.GenerateMessageID().

func (*Client) SetPassive ¶
func (cli *Client) SetPassive(passive bool) error
SetPassive tells the WhatsApp server whether this device is passive or not.

This seems to mostly affect whether the device receives certain events. By default, whatsmeow will automatically do SetPassive(false) after connecting.

func (*Client) SetPrivacySetting ¶
func (cli *Client) SetPrivacySetting(name types.PrivacySettingType, value types.PrivacySetting) (settings types.PrivacySettings, err error)
SetPrivacySetting will set the given privacy setting to the given value. The privacy settings will be fetched from the server after the change and the new settings will be returned. If an error occurs while fetching the new settings, will return an empty struct.

func (*Client) SetProxy ¶
func (cli *Client) SetProxy(proxy Proxy, opts ...SetProxyOptions)
SetProxy sets a HTTP proxy to use for WhatsApp web websocket connections and media uploads/downloads.

Must be called before Connect() to take effect in the websocket connection. If you want to change the proxy after connecting, you must call Disconnect() and then Connect() again manually.

By default, the client will find the proxy from the https_proxy environment variable like Go's net/http does.

To disable reading proxy info from environment variables, explicitly set the proxy to nil:

cli.SetProxy(nil)
To use a different proxy for the websocket and media, pass a function that checks the request path or headers:

cli.SetProxy(func(r *http.Request) (*url.URL, error) {
	if r.URL.Host == "web.whatsapp.com" && r.URL.Path == "/ws/chat" {
		return websocketProxyURL, nil
	} else {
		return mediaProxyURL, nil
	}
})
func (*Client) SetProxyAddress ¶
func (cli *Client) SetProxyAddress(addr string, opts ...SetProxyOptions) error
SetProxyAddress is a helper method that parses a URL string and calls SetProxy or SetSOCKSProxy based on the URL scheme.

Returns an error if url.Parse fails to parse the given address.

func (*Client) SetSOCKSProxy ¶
func (cli *Client) SetSOCKSProxy(px proxy.Dialer, opts ...SetProxyOptions)
SetSOCKSProxy sets a SOCKS5 proxy to use for WhatsApp web websocket connections and media uploads/downloads.

Same details as SetProxy apply, but using a different proxy for the websocket and media is not currently supported.

func (*Client) SetStatusMessage ¶
func (cli *Client) SetStatusMessage(msg string) error
SetStatusMessage updates the current user's status text, which is shown in the "About" section in the user profile.

This is different from the ephemeral status broadcast messages. Use SendMessage to types.StatusBroadcastJID to send such messages.

func (*Client) SetWSDialer ¶
func (cli *Client) SetWSDialer(dialer *websocket.Dialer)
func (*Client) SubscribePresence ¶
func (cli *Client) SubscribePresence(jid types.JID) error
SubscribePresence asks the WhatsApp servers to send presence updates of a specific user to this client.

After subscribing to this event, you should start receiving *events.Presence for that user in normal event handlers.

Also, it seems that the WhatsApp servers require you to be online to receive presence status from other users, so you should mark yourself as online before trying to use this function:

cli.SendPresence(types.PresenceAvailable)
func (*Client) ToggleProxyOnlyForLogin ¶
func (cli *Client) ToggleProxyOnlyForLogin(only bool)
ToggleProxyOnlyForLogin changes whether the proxy set with SetProxy or related methods is only used for the pre-login websocket and not authenticated websockets.

func (*Client) TryFetchPrivacySettings ¶
func (cli *Client) TryFetchPrivacySettings(ignoreCache bool) (*types.PrivacySettings, error)
TryFetchPrivacySettings will fetch the user's privacy settings, either from the in-memory cache or from the server.

func (*Client) UnfollowNewsletter ¶
func (cli *Client) UnfollowNewsletter(jid types.JID) error
UnfollowNewsletter makes the user unfollow (leave) a WhatsApp channel.

func (*Client) UnlinkGroup ¶
func (cli *Client) UnlinkGroup(parent, child types.JID) error
UnlinkGroup removes a child group from a parent community.

func (*Client) UpdateBlocklist ¶
func (cli *Client) UpdateBlocklist(jid types.JID, action events.BlocklistChangeAction) (*types.Blocklist, error)
UpdateBlocklist updates the user's block list and returns the updated list.

func (*Client) UpdateGroupParticipants ¶
func (cli *Client) UpdateGroupParticipants(jid types.JID, participantChanges []types.JID, action ParticipantChange) ([]types.GroupParticipant, error)
UpdateGroupParticipants can be used to add, remove, promote and demote members in a WhatsApp group.

func (*Client) UpdateGroupRequestParticipants ¶
func (cli *Client) UpdateGroupRequestParticipants(jid types.JID, participantChanges []types.JID, action ParticipantRequestChange) ([]types.GroupParticipant, error)
UpdateGroupRequestParticipants can be used to approve or reject requests to join the group.

func (*Client) Upload ¶
func (cli *Client) Upload(ctx context.Context, plaintext []byte, appInfo MediaType) (resp UploadResponse, err error)
Upload uploads the given attachment to WhatsApp servers.

You should copy the fields in the response to the corresponding fields in a protobuf message.

For example, to send an image:

resp, err := cli.Upload(context.Background(), yourImageBytes, whatsmeow.MediaImage)
// handle error

imageMsg := &waE2E.ImageMessage{
	Caption:  proto.String("Hello, world!"),
	Mimetype: proto.String("image/png"), // replace this with the actual mime type
	// you can also optionally add other fields like ContextInfo and JpegThumbnail here

	URL:           &resp.URL,
	DirectPath:    &resp.DirectPath,
	MediaKey:      resp.MediaKey,
	FileEncSHA256: resp.FileEncSHA256,
	FileSHA256:    resp.FileSHA256,
	FileLength:    &resp.FileLength,
}
_, err = cli.SendMessage(context.Background(), targetJID, &waE2E.Message{
	ImageMessage: imageMsg,
})
// handle error again
The same applies to the other message types like DocumentMessage, just replace the struct type and Message field name.

func (*Client) UploadNewsletter ¶
func (cli *Client) UploadNewsletter(ctx context.Context, data []byte, appInfo MediaType) (resp UploadResponse, err error)
UploadNewsletter uploads the given attachment to WhatsApp servers without encrypting it first.

Newsletter media works mostly the same way as normal media, with a few differences: * Since it's unencrypted, there's no MediaKey or FileEncSHA256 fields. * There's a "media handle" that needs to be passed in SendRequestExtra.

Example:

resp, err := cli.UploadNewsletter(context.Background(), yourImageBytes, whatsmeow.MediaImage)
// handle error

imageMsg := &waE2E.ImageMessage{
	// Caption, mime type and other such fields work like normal
	Caption:  proto.String("Hello, world!"),
	Mimetype: proto.String("image/png"),

	// URL and direct path are also there like normal media
	URL:        &resp.URL,
	DirectPath: &resp.DirectPath,
	FileSHA256: resp.FileSHA256,
	FileLength: &resp.FileLength,
	// Newsletter media isn't encrypted, so the media key and file enc sha fields are not applicable
}
_, err = cli.SendMessage(context.Background(), newsletterJID, &waE2E.Message{
	ImageMessage: imageMsg,
}, whatsmeow.SendRequestExtra{
	// Unlike normal media, newsletters also include a "media handle" in the send request.
	MediaHandle: resp.Handle,
})
// handle error again
func (*Client) UploadNewsletterReader ¶
func (cli *Client) UploadNewsletterReader(ctx context.Context, data io.ReadSeeker, appInfo MediaType) (resp UploadResponse, err error)
UploadNewsletterReader uploads the given attachment to WhatsApp servers without encrypting it first.

This is otherwise identical to [UploadNewsletter], but it reads the plaintext from an io.Reader instead of a byte slice. Unlike [UploadReader], this does not require a temporary file. However, the data needs to be hashed first, so an io.ReadSeeker is required to be able to read the data twice.

func (*Client) UploadReader ¶
func (cli *Client) UploadReader(ctx context.Context, plaintext io.Reader, tempFile io.ReadWriteSeeker, appInfo MediaType) (resp UploadResponse, err error)
UploadReader uploads the given attachment to WhatsApp servers.

This is otherwise identical to [Upload], but it reads the plaintext from an io.Reader instead of a byte slice. A temporary file is required for the encryption process. If tempFile is nil, a temporary file will be created and deleted after the upload.

To use only one file, pass the same file as both plaintext and tempFile. This will cause the file to be overwritten with encrypted data.

func (*Client) WaitForConnection ¶
func (cli *Client) WaitForConnection(timeout time.Duration) bool
type CreateNewsletterParams ¶
type CreateNewsletterParams struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Picture     []byte `json:"picture,omitempty"`
}
type DangerousInfoQuery ¶
type DangerousInfoQuery = infoQuery
type DangerousInfoQueryType ¶
type DangerousInfoQueryType = infoQueryType
type DangerousInternalClient ¶
type DangerousInternalClient struct {
	// contains filtered or unexported fields
}
func (*DangerousInternalClient) AddRecentMessage ¶
func (int *DangerousInternalClient) AddRecentMessage(to types.JID, id types.MessageID, wa *waE2E.Message, fb *waMsgApplication.MessageApplication)
func (*DangerousInternalClient) AutoReconnect ¶
func (int *DangerousInternalClient) AutoReconnect()
func (*DangerousInternalClient) CancelDelayedRequestFromPhone ¶
func (int *DangerousInternalClient) CancelDelayedRequestFromPhone(msgID types.MessageID)
func (*DangerousInternalClient) CancelResponse ¶
func (int *DangerousInternalClient) CancelResponse(reqID string, ch chan *waBinary.Node)
func (*DangerousInternalClient) ClearResponseWaiters ¶
func (int *DangerousInternalClient) ClearResponseWaiters(node *waBinary.Node)
func (*DangerousInternalClient) ClearUntrustedIdentity ¶
func (int *DangerousInternalClient) ClearUntrustedIdentity(target types.JID)
func (*DangerousInternalClient) CloseSocketWaitChan ¶
func (int *DangerousInternalClient) CloseSocketWaitChan()
func (*DangerousInternalClient) DecryptBotMessage ¶
func (int *DangerousInternalClient) DecryptBotMessage(messageSecret []byte, msMsg messageEncryptedSecret, messageID types.MessageID, targetSenderJID types.JID, info *types.MessageInfo) ([]byte, error)
func (*DangerousInternalClient) DecryptDM ¶
func (int *DangerousInternalClient) DecryptDM(child *waBinary.Node, from types.JID, isPreKey bool) ([]byte, error)
func (*DangerousInternalClient) DecryptGroupMsg ¶
func (int *DangerousInternalClient) DecryptGroupMsg(child *waBinary.Node, from types.JID, chat types.JID) ([]byte, error)
func (*DangerousInternalClient) DecryptMessages ¶
func (int *DangerousInternalClient) DecryptMessages(info *types.MessageInfo, node *waBinary.Node)
func (*DangerousInternalClient) DecryptMsgSecret ¶
func (int *DangerousInternalClient) DecryptMsgSecret(msg *events.Message, useCase MsgSecretType, encrypted messageEncryptedSecret, origMsgKey *waCommon.MessageKey) ([]byte, error)
func (*DangerousInternalClient) DelayedRequestMessageFromPhone ¶
func (int *DangerousInternalClient) DelayedRequestMessageFromPhone(info *types.MessageInfo)
func (*DangerousInternalClient) DispatchAppState ¶
func (int *DangerousInternalClient) DispatchAppState(mutation appstate.Mutation, fullSync bool, emitOnFullSync bool)
func (*DangerousInternalClient) DispatchEvent ¶
func (int *DangerousInternalClient) DispatchEvent(evt any)
func (*DangerousInternalClient) DoHandshake ¶
func (int *DangerousInternalClient) DoHandshake(fs *socket.FrameSocket, ephemeralKP keys.KeyPair) error
func (*DangerousInternalClient) DoMediaDownloadRequest ¶
func (int *DangerousInternalClient) DoMediaDownloadRequest(url string) (*http.Response, error)
func (*DangerousInternalClient) DownloadAndDecrypt ¶
func (int *DangerousInternalClient) DownloadAndDecrypt(url string, mediaKey []byte, appInfo MediaType, fileLength int, fileEncSHA256, fileSHA256 []byte) (data []byte, err error)
func (*DangerousInternalClient) DownloadAndDecryptToFile ¶
func (int *DangerousInternalClient) DownloadAndDecryptToFile(url string, mediaKey []byte, appInfo MediaType, fileLength int, fileEncSHA256, fileSHA256 []byte, file File) error
func (*DangerousInternalClient) DownloadEncryptedMedia ¶
func (int *DangerousInternalClient) DownloadEncryptedMedia(url string, checksum []byte) (file, mac []byte, err error)
func (*DangerousInternalClient) DownloadEncryptedMediaToFile ¶
func (int *DangerousInternalClient) DownloadEncryptedMediaToFile(url string, checksum []byte, file File) ([]byte, error)
func (*DangerousInternalClient) DownloadExternalAppStateBlob ¶
func (int *DangerousInternalClient) DownloadExternalAppStateBlob(ref *waServerSync.ExternalBlobReference) ([]byte, error)
func (*DangerousInternalClient) DownloadMedia ¶
func (int *DangerousInternalClient) DownloadMedia(url string) ([]byte, error)
func (*DangerousInternalClient) DownloadMediaToFile ¶
func (int *DangerousInternalClient) DownloadMediaToFile(url string, file io.Writer) (int64, []byte, error)
func (*DangerousInternalClient) DownloadPossiblyEncryptedMediaWithRetries ¶
func (int *DangerousInternalClient) DownloadPossiblyEncryptedMediaWithRetries(url string, checksum []byte) (file, mac []byte, err error)
func (*DangerousInternalClient) DownloadPossiblyEncryptedMediaWithRetriesToFile ¶
func (int *DangerousInternalClient) DownloadPossiblyEncryptedMediaWithRetriesToFile(url string, checksum []byte, file File) (mac []byte, err error)
func (*DangerousInternalClient) EncryptMessageForDevice ¶
func (int *DangerousInternalClient) EncryptMessageForDevice(plaintext []byte, to types.JID, bundle *prekey.Bundle, extraAttrs waBinary.Attrs) (*waBinary.Node, bool, error)
func (*DangerousInternalClient) EncryptMessageForDeviceAndWrap ¶
func (int *DangerousInternalClient) EncryptMessageForDeviceAndWrap(plaintext []byte, to types.JID, bundle *prekey.Bundle, encAttrs waBinary.Attrs) (*waBinary.Node, bool, error)
func (*DangerousInternalClient) EncryptMessageForDeviceAndWrapV3 ¶
func (int *DangerousInternalClient) EncryptMessageForDeviceAndWrapV3(payload *waMsgTransport.MessageTransport_Payload, skdm *waMsgTransport.MessageTransport_Protocol_Ancillary_SenderKeyDistributionMessage, dsm *waMsgTransport.MessageTransport_Protocol_Integral_DeviceSentMessage, to types.JID, bundle *prekey.Bundle, encAttrs waBinary.Attrs) (*waBinary.Node, error)
func (*DangerousInternalClient) EncryptMessageForDeviceV3 ¶
func (int *DangerousInternalClient) EncryptMessageForDeviceV3(payload *waMsgTransport.MessageTransport_Payload, skdm *waMsgTransport.MessageTransport_Protocol_Ancillary_SenderKeyDistributionMessage, dsm *waMsgTransport.MessageTransport_Protocol_Integral_DeviceSentMessage, to types.JID, bundle *prekey.Bundle, extraAttrs waBinary.Attrs) (*waBinary.Node, error)
func (*DangerousInternalClient) EncryptMessageForDevices ¶
func (int *DangerousInternalClient) EncryptMessageForDevices(ctx context.Context, allDevices []types.JID, ownID types.JID, id string, msgPlaintext, dsmPlaintext []byte, encAttrs waBinary.Attrs) ([]waBinary.Node, bool)
func (*DangerousInternalClient) EncryptMessageForDevicesV3 ¶
func (int *DangerousInternalClient) EncryptMessageForDevicesV3(ctx context.Context, allDevices []types.JID, ownID types.JID, id string, payload *waMsgTransport.MessageTransport_Payload, skdm *waMsgTransport.MessageTransport_Protocol_Ancillary_SenderKeyDistributionMessage, dsm *waMsgTransport.MessageTransport_Protocol_Integral_DeviceSentMessage, encAttrs waBinary.Attrs) []waBinary.Node
func (*DangerousInternalClient) EncryptMsgSecret ¶
func (int *DangerousInternalClient) EncryptMsgSecret(chat, origSender types.JID, origMsgID types.MessageID, useCase MsgSecretType, plaintext []byte) (ciphertext, iv []byte, err error)
func (*DangerousInternalClient) ExpectDisconnect ¶
func (int *DangerousInternalClient) ExpectDisconnect()
func (*DangerousInternalClient) FetchAppStatePatches ¶
func (int *DangerousInternalClient) FetchAppStatePatches(name appstate.WAPatchName, fromVersion uint64, snapshot bool) (*appstate.PatchList, error)
func (*DangerousInternalClient) FetchPreKeys ¶
func (int *DangerousInternalClient) FetchPreKeys(ctx context.Context, users []types.JID) (map[types.JID]preKeyResp, error)
func (*DangerousInternalClient) FilterContacts ¶
func (int *DangerousInternalClient) FilterContacts(mutations []appstate.Mutation) ([]appstate.Mutation, []store.ContactEntry)
func (*DangerousInternalClient) GenerateRequestID ¶
func (int *DangerousInternalClient) GenerateRequestID() string
func (*DangerousInternalClient) GetBroadcastListParticipants ¶
func (int *DangerousInternalClient) GetBroadcastListParticipants(jid types.JID) ([]types.JID, error)
func (*DangerousInternalClient) GetFBIDDevices ¶
func (int *DangerousInternalClient) GetFBIDDevices(ctx context.Context, jids []types.JID) (*waBinary.Node, error)
func (*DangerousInternalClient) GetGroupInfo ¶
func (int *DangerousInternalClient) GetGroupInfo(ctx context.Context, jid types.JID, lockParticipantCache bool) (*types.GroupInfo, error)
func (*DangerousInternalClient) GetGroupMembers ¶
func (int *DangerousInternalClient) GetGroupMembers(ctx context.Context, jid types.JID) ([]types.JID, error)
func (*DangerousInternalClient) GetMessageContent ¶
func (int *DangerousInternalClient) GetMessageContent(baseNode waBinary.Node, message *waE2E.Message, msgAttrs waBinary.Attrs, includeIdentity bool, botNode *waBinary.Node) []waBinary.Node
func (*DangerousInternalClient) GetMessageForRetry ¶
func (int *DangerousInternalClient) GetMessageForRetry(receipt *events.Receipt, messageID types.MessageID) (RecentMessage, error)
func (*DangerousInternalClient) GetNewsletterInfo ¶
func (int *DangerousInternalClient) GetNewsletterInfo(input map[string]any, fetchViewerMeta bool) (*types.NewsletterMetadata, error)
func (*DangerousInternalClient) GetOwnID ¶
func (int *DangerousInternalClient) GetOwnID() types.JID
func (*DangerousInternalClient) GetRecentMessage ¶
func (int *DangerousInternalClient) GetRecentMessage(to types.JID, id types.MessageID) RecentMessage
func (*DangerousInternalClient) GetServerPreKeyCount ¶
func (int *DangerousInternalClient) GetServerPreKeyCount() (int, error)
func (*DangerousInternalClient) GetSocketWaitChan ¶
func (int *DangerousInternalClient) GetSocketWaitChan() <-chan struct{}
func (*DangerousInternalClient) GetStatusBroadcastRecipients ¶
func (int *DangerousInternalClient) GetStatusBroadcastRecipients() ([]types.JID, error)
func (*DangerousInternalClient) HandleAccountSyncNotification ¶
func (int *DangerousInternalClient) HandleAccountSyncNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleAppStateNotification ¶
func (int *DangerousInternalClient) HandleAppStateNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleAppStateSyncKeyShare ¶
func (int *DangerousInternalClient) HandleAppStateSyncKeyShare(keys *waE2E.AppStateSyncKeyShare)
func (*DangerousInternalClient) HandleBlocklist ¶
func (int *DangerousInternalClient) HandleBlocklist(node *waBinary.Node)
func (*DangerousInternalClient) HandleCallEvent ¶
func (int *DangerousInternalClient) HandleCallEvent(node *waBinary.Node)
func (*DangerousInternalClient) HandleChatState ¶
func (int *DangerousInternalClient) HandleChatState(node *waBinary.Node)
func (*DangerousInternalClient) HandleCodePairNotification ¶
func (int *DangerousInternalClient) HandleCodePairNotification(parentNode *waBinary.Node) error
func (*DangerousInternalClient) HandleConnectFailure ¶
func (int *DangerousInternalClient) HandleConnectFailure(node *waBinary.Node)
func (*DangerousInternalClient) HandleConnectSuccess ¶
func (int *DangerousInternalClient) HandleConnectSuccess(node *waBinary.Node)
func (*DangerousInternalClient) HandleDecryptedArmadillo ¶
func (int *DangerousInternalClient) HandleDecryptedArmadillo(info *types.MessageInfo, decrypted []byte, retryCount int) bool
func (*DangerousInternalClient) HandleDecryptedMessage ¶
func (int *DangerousInternalClient) HandleDecryptedMessage(info *types.MessageInfo, msg *waE2E.Message, retryCount int)
func (*DangerousInternalClient) HandleDeviceNotification ¶
func (int *DangerousInternalClient) HandleDeviceNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleEncryptNotification ¶
func (int *DangerousInternalClient) HandleEncryptNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleEncryptedMessage ¶
func (int *DangerousInternalClient) HandleEncryptedMessage(node *waBinary.Node)
func (*DangerousInternalClient) HandleFBDeviceNotification ¶
func (int *DangerousInternalClient) HandleFBDeviceNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleFrame ¶
func (int *DangerousInternalClient) HandleFrame(data []byte)
func (*DangerousInternalClient) HandleGroupedReceipt ¶
func (int *DangerousInternalClient) HandleGroupedReceipt(partialReceipt events.Receipt, participants *waBinary.Node)
func (*DangerousInternalClient) HandleHistoricalPushNames ¶
func (int *DangerousInternalClient) HandleHistoricalPushNames(names []*waHistorySync.Pushname)
func (*DangerousInternalClient) HandleHistorySyncNotification ¶
func (int *DangerousInternalClient) HandleHistorySyncNotification(notif *waE2E.HistorySyncNotification)
func (*DangerousInternalClient) HandleHistorySyncNotificationLoop ¶
func (int *DangerousInternalClient) HandleHistorySyncNotificationLoop()
func (*DangerousInternalClient) HandleIB ¶
func (int *DangerousInternalClient) HandleIB(node *waBinary.Node)
func (*DangerousInternalClient) HandleIQ ¶
func (int *DangerousInternalClient) HandleIQ(node *waBinary.Node)
func (*DangerousInternalClient) HandleMediaRetryNotification ¶
func (int *DangerousInternalClient) HandleMediaRetryNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleMexNotification ¶
func (int *DangerousInternalClient) HandleMexNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleNewsletterNotification ¶
func (int *DangerousInternalClient) HandleNewsletterNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleNotification ¶
func (int *DangerousInternalClient) HandleNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleOwnDevicesNotification ¶
func (int *DangerousInternalClient) HandleOwnDevicesNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandlePair ¶
func (int *DangerousInternalClient) HandlePair(deviceIdentityBytes []byte, reqID, businessName, platform string, jid types.JID) error
func (*DangerousInternalClient) HandlePairDevice ¶
func (int *DangerousInternalClient) HandlePairDevice(node *waBinary.Node)
func (*DangerousInternalClient) HandlePairSuccess ¶
func (int *DangerousInternalClient) HandlePairSuccess(node *waBinary.Node)
func (*DangerousInternalClient) HandlePictureNotification ¶
func (int *DangerousInternalClient) HandlePictureNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandlePlaceholderResendResponse ¶
func (int *DangerousInternalClient) HandlePlaceholderResendResponse(msg *waE2E.PeerDataOperationRequestResponseMessage)
func (*DangerousInternalClient) HandlePlaintextMessage ¶
func (int *DangerousInternalClient) HandlePlaintextMessage(info *types.MessageInfo, node *waBinary.Node)
func (*DangerousInternalClient) HandlePresence ¶
func (int *DangerousInternalClient) HandlePresence(node *waBinary.Node)
func (*DangerousInternalClient) HandlePrivacySettingsNotification ¶
func (int *DangerousInternalClient) HandlePrivacySettingsNotification(privacyNode *waBinary.Node)
func (*DangerousInternalClient) HandlePrivacyTokenNotification ¶
func (int *DangerousInternalClient) HandlePrivacyTokenNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleProtocolMessage ¶
func (int *DangerousInternalClient) HandleProtocolMessage(info *types.MessageInfo, msg *waE2E.Message)
func (*DangerousInternalClient) HandleReceipt ¶
func (int *DangerousInternalClient) HandleReceipt(node *waBinary.Node)
func (*DangerousInternalClient) HandleRetryReceipt ¶
func (int *DangerousInternalClient) HandleRetryReceipt(receipt *events.Receipt, node *waBinary.Node) error
func (*DangerousInternalClient) HandleSenderKeyDistributionMessage ¶
func (int *DangerousInternalClient) HandleSenderKeyDistributionMessage(chat, from types.JID, axolotlSKDM []byte)
func (*DangerousInternalClient) HandleStatusNotification ¶
func (int *DangerousInternalClient) HandleStatusNotification(node *waBinary.Node)
func (*DangerousInternalClient) HandleStreamError ¶
func (int *DangerousInternalClient) HandleStreamError(node *waBinary.Node)
func (*DangerousInternalClient) HandlerQueueLoop ¶
func (int *DangerousInternalClient) HandlerQueueLoop(ctx context.Context)
func (*DangerousInternalClient) IsExpectedDisconnect ¶
func (int *DangerousInternalClient) IsExpectedDisconnect() bool
func (*DangerousInternalClient) KeepAliveLoop ¶
func (int *DangerousInternalClient) KeepAliveLoop(ctx context.Context)
func (*DangerousInternalClient) MakeDeviceIdentityNode ¶
func (int *DangerousInternalClient) MakeDeviceIdentityNode() waBinary.Node
func (*DangerousInternalClient) MakeQRData ¶
func (int *DangerousInternalClient) MakeQRData(ref string) string
func (*DangerousInternalClient) MaybeDeferredAck ¶
func (int *DangerousInternalClient) MaybeDeferredAck(node *waBinary.Node) func()
func (*DangerousInternalClient) OnDisconnect ¶
func (int *DangerousInternalClient) OnDisconnect(ns *socket.NoiseSocket, remote bool)
func (*DangerousInternalClient) ParseBlocklist ¶
func (int *DangerousInternalClient) ParseBlocklist(node *waBinary.Node) *types.Blocklist
func (*DangerousInternalClient) ParseBusinessProfile ¶
func (int *DangerousInternalClient) ParseBusinessProfile(node *waBinary.Node) (*types.BusinessProfile, error)
func (*DangerousInternalClient) ParseGroupChange ¶
func (int *DangerousInternalClient) ParseGroupChange(node *waBinary.Node) (*events.GroupInfo, error)
func (*DangerousInternalClient) ParseGroupCreate ¶
func (int *DangerousInternalClient) ParseGroupCreate(node *waBinary.Node) (*events.JoinedGroup, error)
func (*DangerousInternalClient) ParseGroupNode ¶
func (int *DangerousInternalClient) ParseGroupNode(groupNode *waBinary.Node) (*types.GroupInfo, error)
func (*DangerousInternalClient) ParseGroupNotification ¶
func (int *DangerousInternalClient) ParseGroupNotification(node *waBinary.Node) (any, error)
func (*DangerousInternalClient) ParseMessageInfo ¶
func (int *DangerousInternalClient) ParseMessageInfo(node *waBinary.Node) (*types.MessageInfo, error)
func (*DangerousInternalClient) ParseMessageSource ¶
func (int *DangerousInternalClient) ParseMessageSource(node *waBinary.Node, requireParticipant bool) (source types.MessageSource, err error)
func (*DangerousInternalClient) ParseMsgBotInfo ¶
func (int *DangerousInternalClient) ParseMsgBotInfo(node waBinary.Node) (botInfo types.MsgBotInfo, err error)
func (*DangerousInternalClient) ParseMsgMetaInfo ¶
func (int *DangerousInternalClient) ParseMsgMetaInfo(node waBinary.Node) (metaInfo types.MsgMetaInfo, err error)
func (*DangerousInternalClient) ParseNewsletterMessages ¶
func (int *DangerousInternalClient) ParseNewsletterMessages(node *waBinary.Node) []*types.NewsletterMessage
func (*DangerousInternalClient) ParsePrivacySettings ¶
func (int *DangerousInternalClient) ParsePrivacySettings(privacyNode *waBinary.Node, settings *types.PrivacySettings) *events.PrivacySettings
func (*DangerousInternalClient) ParseReceipt ¶
func (int *DangerousInternalClient) ParseReceipt(node *waBinary.Node) (*events.Receipt, error)
func (*DangerousInternalClient) PrepareMessageNode ¶
func (int *DangerousInternalClient) PrepareMessageNode(ctx context.Context, to, ownID types.JID, id types.MessageID, message *waE2E.Message, participants []types.JID, plaintext, dsmPlaintext []byte, timings *MessageDebugTimings, botNode *waBinary.Node) (*waBinary.Node, []types.JID, error)
func (*DangerousInternalClient) PrepareMessageNodeV3 ¶
func (int *DangerousInternalClient) PrepareMessageNodeV3(ctx context.Context, to, ownID types.JID, id types.MessageID, payload *waMsgTransport.MessageTransport_Payload, skdm *waMsgTransport.MessageTransport_Protocol_Ancillary_SenderKeyDistributionMessage, msgAttrs messageAttrs, frankingTag []byte, participants []types.JID, timings *MessageDebugTimings) (*waBinary.Node, []types.JID, error)
func (*DangerousInternalClient) PreparePeerMessageNode ¶
func (int *DangerousInternalClient) PreparePeerMessageNode(to types.JID, id types.MessageID, message *waE2E.Message, timings *MessageDebugTimings) (*waBinary.Node, error)
func (*DangerousInternalClient) ProcessProtocolParts ¶
func (int *DangerousInternalClient) ProcessProtocolParts(info *types.MessageInfo, msg *waE2E.Message)
func (*DangerousInternalClient) QueryMediaConn ¶
func (int *DangerousInternalClient) QueryMediaConn() (*MediaConn, error)
func (*DangerousInternalClient) RawUpload ¶
func (int *DangerousInternalClient) RawUpload(ctx context.Context, dataToUpload io.Reader, uploadSize uint64, fileHash []byte, appInfo MediaType, newsletter bool, resp *UploadResponse) error
func (*DangerousInternalClient) ReceiveResponse ¶
func (int *DangerousInternalClient) ReceiveResponse(data *waBinary.Node) bool
func (*DangerousInternalClient) RefreshMediaConn ¶
func (int *DangerousInternalClient) RefreshMediaConn(force bool) (*MediaConn, error)
func (*DangerousInternalClient) RequestAppStateKeys ¶
func (int *DangerousInternalClient) RequestAppStateKeys(ctx context.Context, rawKeyIDs [][]byte)
func (*DangerousInternalClient) RequestMissingAppStateKeys ¶
func (int *DangerousInternalClient) RequestMissingAppStateKeys(ctx context.Context, patches *appstate.PatchList)
func (*DangerousInternalClient) ResetExpectedDisconnect ¶
func (int *DangerousInternalClient) ResetExpectedDisconnect()
func (*DangerousInternalClient) RetryFrame ¶
func (int *DangerousInternalClient) RetryFrame(reqType, id string, data []byte, origResp *waBinary.Node, ctx context.Context, timeout time.Duration) (*waBinary.Node, error)
func (*DangerousInternalClient) SendAck ¶
func (int *DangerousInternalClient) SendAck(node *waBinary.Node)
func (*DangerousInternalClient) SendDM ¶
func (int *DangerousInternalClient) SendDM(ctx context.Context, to, ownID types.JID, id types.MessageID, message *waE2E.Message, timings *MessageDebugTimings, botNode *waBinary.Node) ([]byte, error)
func (*DangerousInternalClient) SendDMV3 ¶
func (int *DangerousInternalClient) SendDMV3(ctx context.Context, to, ownID types.JID, id types.MessageID, messageApp []byte, msgAttrs messageAttrs, frankingTag []byte, timings *MessageDebugTimings) ([]byte, string, error)
func (*DangerousInternalClient) SendGroup ¶
func (int *DangerousInternalClient) SendGroup(ctx context.Context, to, ownID types.JID, id types.MessageID, message *waE2E.Message, timings *MessageDebugTimings, botNode *waBinary.Node) (string, []byte, error)
func (*DangerousInternalClient) SendGroupIQ ¶
func (int *DangerousInternalClient) SendGroupIQ(ctx context.Context, iqType infoQueryType, jid types.JID, content waBinary.Node) (*waBinary.Node, error)
func (*DangerousInternalClient) SendGroupV3 ¶
func (int *DangerousInternalClient) SendGroupV3(ctx context.Context, to, ownID types.JID, id types.MessageID, messageApp []byte, msgAttrs messageAttrs, frankingTag []byte, timings *MessageDebugTimings) (string, []byte, error)
func (*DangerousInternalClient) SendIQ ¶
func (int *DangerousInternalClient) SendIQ(query infoQuery) (*waBinary.Node, error)
func (*DangerousInternalClient) SendIQAsync ¶
func (int *DangerousInternalClient) SendIQAsync(query infoQuery) (<-chan *waBinary.Node, error)
func (*DangerousInternalClient) SendIQAsyncAndGetData ¶
func (int *DangerousInternalClient) SendIQAsyncAndGetData(query *infoQuery) (<-chan *waBinary.Node, []byte, error)
func (*DangerousInternalClient) SendKeepAlive ¶
func (int *DangerousInternalClient) SendKeepAlive(ctx context.Context) (isSuccess, shouldContinue bool)
func (*DangerousInternalClient) SendMessageReceipt ¶
func (int *DangerousInternalClient) SendMessageReceipt(info *types.MessageInfo)
func (*DangerousInternalClient) SendMexIQ ¶
func (int *DangerousInternalClient) SendMexIQ(ctx context.Context, queryID string, variables any) (json.RawMessage, error)
func (*DangerousInternalClient) SendNewsletter ¶
func (int *DangerousInternalClient) SendNewsletter(to types.JID, id types.MessageID, message *waE2E.Message, mediaID string, timings *MessageDebugTimings) ([]byte, error)
func (*DangerousInternalClient) SendNode ¶
func (int *DangerousInternalClient) SendNode(node waBinary.Node) error
func (*DangerousInternalClient) SendNodeAndGetData ¶
func (int *DangerousInternalClient) SendNodeAndGetData(node waBinary.Node) ([]byte, error)
func (*DangerousInternalClient) SendPairError ¶
func (int *DangerousInternalClient) SendPairError(id string, code int, text string)
func (*DangerousInternalClient) SendPeerMessage ¶
func (int *DangerousInternalClient) SendPeerMessage(to types.JID, id types.MessageID, message *waE2E.Message, timings *MessageDebugTimings) ([]byte, error)
func (*DangerousInternalClient) SendProtocolMessageReceipt ¶
func (int *DangerousInternalClient) SendProtocolMessageReceipt(id types.MessageID, msgType types.ReceiptType)
func (*DangerousInternalClient) SendRetryReceipt ¶
func (int *DangerousInternalClient) SendRetryReceipt(node *waBinary.Node, info *types.MessageInfo, forceIncludeIdentity bool)
func (*DangerousInternalClient) ShouldRecreateSession ¶
func (int *DangerousInternalClient) ShouldRecreateSession(retryCount int, jid types.JID) (reason string, recreate bool)
func (*DangerousInternalClient) StoreHistoricalMessageSecrets ¶
func (int *DangerousInternalClient) StoreHistoricalMessageSecrets(conversations []*waHistorySync.Conversation)
func (*DangerousInternalClient) StoreMessageSecret ¶
func (int *DangerousInternalClient) StoreMessageSecret(info *types.MessageInfo, msg *waE2E.Message)
func (*DangerousInternalClient) TryHandleCodePairNotification ¶
func (int *DangerousInternalClient) TryHandleCodePairNotification(parentNode *waBinary.Node)
func (*DangerousInternalClient) UnlockedDisconnect ¶
func (int *DangerousInternalClient) UnlockedDisconnect()
func (*DangerousInternalClient) UpdateBusinessName ¶
func (int *DangerousInternalClient) UpdateBusinessName(user types.JID, messageInfo *types.MessageInfo, name string)
func (*DangerousInternalClient) UpdateGroupParticipantCache ¶
func (int *DangerousInternalClient) UpdateGroupParticipantCache(evt *events.GroupInfo)
func (*DangerousInternalClient) UpdatePushName ¶
func (int *DangerousInternalClient) UpdatePushName(user types.JID, messageInfo *types.MessageInfo, name string)
func (*DangerousInternalClient) UploadPreKeys ¶
func (int *DangerousInternalClient) UploadPreKeys()
func (*DangerousInternalClient) Usync ¶
func (int *DangerousInternalClient) Usync(ctx context.Context, jids []types.JID, mode, context string, query []waBinary.Node, extra ...UsyncQueryExtras) (*waBinary.Node, error)
func (*DangerousInternalClient) WaitResponse ¶
func (int *DangerousInternalClient) WaitResponse(reqID string) chan *waBinary.Node
type DisconnectedError ¶
type DisconnectedError struct {
	Action string
	Node   *waBinary.Node
}
DisconnectedError is returned if the websocket disconnects before an info query or other request gets a response.

func (*DisconnectedError) Error ¶
func (err *DisconnectedError) Error() string
func (*DisconnectedError) Is ¶
func (err *DisconnectedError) Is(other error) bool
type DownloadHTTPError ¶
type DownloadHTTPError struct {
	*http.Response
}
func (DownloadHTTPError) Error ¶
func (dhe DownloadHTTPError) Error() string
func (DownloadHTTPError) Is ¶
func (dhe DownloadHTTPError) Is(other error) bool
type DownloadableMessage ¶
type DownloadableMessage interface {
	GetDirectPath() string
	GetMediaKey() []byte
	GetFileSHA256() []byte
	GetFileEncSHA256() []byte
}
DownloadableMessage represents a protobuf message that contains attachment info.

All of the downloadable messages inside a Message struct implement this interface (ImageMessage, VideoMessage, AudioMessage, DocumentMessage, StickerMessage).

type DownloadableThumbnail ¶
type DownloadableThumbnail interface {
	proto.Message
	GetThumbnailDirectPath() string
	GetThumbnailSHA256() []byte
	GetThumbnailEncSHA256() []byte
	GetMediaKey() []byte
}
DownloadableThumbnail represents a protobuf message that contains a thumbnail attachment.

This is primarily meant for link preview thumbnails in ExtendedTextMessage.

type ElementMissingError ¶
type ElementMissingError struct {
	Tag string
	In  string
}
ElementMissingError is returned by various functions that parse XML elements when a required element is missing.

func (*ElementMissingError) Error ¶
func (eme *ElementMissingError) Error() string
type EventHandler ¶
type EventHandler func(evt any)
EventHandler is a function that can handle events from WhatsApp.

type FCMPushConfig ¶
type FCMPushConfig struct {
	Token string `json:"token"`
}
func (*FCMPushConfig) GetPushConfigAttrs ¶
func (fpc *FCMPushConfig) GetPushConfigAttrs() waBinary.Attrs
type File ¶
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.ReaderAt
	io.WriterAt
	Truncate(size int64) error
	Stat() (os.FileInfo, error)
}
type GetNewsletterMessagesParams ¶
type GetNewsletterMessagesParams struct {
	Count  int
	Before types.MessageServerID
}
type GetNewsletterUpdatesParams ¶
type GetNewsletterUpdatesParams struct {
	Count int
	Since time.Time
	After types.MessageServerID
}
type GetProfilePictureParams ¶
type GetProfilePictureParams struct {
	Preview     bool
	ExistingID  string
	IsCommunity bool
}
type IQError ¶
type IQError struct {
	Code      int
	Text      string
	ErrorNode *waBinary.Node
	RawNode   *waBinary.Node
}
IQError is a generic error container for info queries

func (*IQError) Error ¶
func (iqe *IQError) Error() string
func (*IQError) Is ¶
func (iqe *IQError) Is(other error) bool
type MediaConn ¶
type MediaConn struct {
	Auth       string
	AuthTTL    int
	TTL        int
	MaxBuckets int
	FetchedAt  time.Time
	Hosts      []MediaConnHost
}
MediaConn contains a list of WhatsApp servers from which attachments can be downloaded from.

func (*MediaConn) Expiry ¶
func (mc *MediaConn) Expiry() time.Time
Expiry returns the time when the MediaConn expires.

type MediaConnHost ¶
type MediaConnHost struct {
	Hostname string
}
MediaConnHost represents a single host to download media from.

type MediaType ¶
type MediaType string
MediaType represents a type of uploaded file on WhatsApp. The value is the key which is used as a part of generating the encryption keys.

const (
	MediaImage    MediaType = "WhatsApp Image Keys"
	MediaVideo    MediaType = "WhatsApp Video Keys"
	MediaAudio    MediaType = "WhatsApp Audio Keys"
	MediaDocument MediaType = "WhatsApp Document Keys"
	MediaHistory  MediaType = "WhatsApp History Keys"
	MediaAppState MediaType = "WhatsApp App State Keys"

	MediaLinkThumbnail MediaType = "WhatsApp Link Thumbnail Keys"
)
The known media types

func GetMediaType ¶
func GetMediaType(msg DownloadableMessage) MediaType
GetMediaType returns the MediaType value corresponding to the given protobuf message.

type MediaTypeable ¶
type MediaTypeable interface {
	GetMediaType() MediaType
}
type MessageDebugTimings ¶
type MessageDebugTimings struct {
	Queue time.Duration

	Marshal         time.Duration
	GetParticipants time.Duration
	GetDevices      time.Duration
	GroupEncrypt    time.Duration
	PeerEncrypt     time.Duration

	Send  time.Duration
	Resp  time.Duration
	Retry time.Duration
}
func (MessageDebugTimings) MarshalZerologObject ¶
func (mdt MessageDebugTimings) MarshalZerologObject(evt *zerolog.Event)
type MessengerConfig ¶
type MessengerConfig struct {
	UserAgent    string
	BaseURL      string
	WebsocketURL string
}
type MsgSecretType ¶
type MsgSecretType string
const (
	EncSecretPollVote MsgSecretType = "Poll Vote"
	EncSecretReaction MsgSecretType = "Enc Reaction"
	EncSecretBotMsg   MsgSecretType = "Bot Message"
)
type PairClientType ¶
type PairClientType int
PairClientType is the type of client to use with PairCode. The type is automatically filled based on store.DeviceProps.PlatformType (which is what QR login uses).

const (
	PairClientUnknown PairClientType = iota
	PairClientChrome
	PairClientEdge
	PairClientFirefox
	PairClientIE
	PairClientOpera
	PairClientSafari
	PairClientElectron
	PairClientUWP
	PairClientOtherWebClient
)
type PairDatabaseError ¶
type PairDatabaseError struct {
	Message string
	DBErr   error
}
PairDatabaseError is included in an events.PairError if the pairing failed due to being unable to save the credentials to the device store.

func (*PairDatabaseError) Error ¶
func (err *PairDatabaseError) Error() string
func (*PairDatabaseError) Unwrap ¶
func (err *PairDatabaseError) Unwrap() error
type PairProtoError ¶
type PairProtoError struct {
	Message  string
	ProtoErr error
}
PairProtoError is included in an events.PairError if the pairing failed due to a protobuf error.

func (*PairProtoError) Error ¶
func (err *PairProtoError) Error() string
func (*PairProtoError) Unwrap ¶
func (err *PairProtoError) Unwrap() error
type ParticipantChange ¶
type ParticipantChange string
const (
	ParticipantChangeAdd     ParticipantChange = "add"
	ParticipantChangeRemove  ParticipantChange = "remove"
	ParticipantChangePromote ParticipantChange = "promote"
	ParticipantChangeDemote  ParticipantChange = "demote"
)
type ParticipantRequestChange ¶
type ParticipantRequestChange string
const (
	ParticipantChangeApprove ParticipantRequestChange = "approve"
	ParticipantChangeReject  ParticipantRequestChange = "reject"
)
type Proxy ¶
type Proxy = func(*http.Request) (*url.URL, error)
type PushConfig ¶
type PushConfig interface {
	GetPushConfigAttrs() waBinary.Attrs
}
type QRChannelItem ¶
type QRChannelItem struct {
	// The type of event, "code" for new QR codes (see Code field) and "error" for pairing errors (see Error) field.
	// For non-code/error events, you can just compare the whole item to the event variables (like QRChannelSuccess).
	Event string
	// If the item is a pair error, then this field contains the error message.
	Error error
	// If the item is a new code, then this field contains the raw data.
	Code string
	// The timeout after which the next code will be sent down the channel.
	Timeout time.Duration
}
type RecentMessage ¶
type RecentMessage struct {
	// contains filtered or unexported fields
}
func (RecentMessage) IsEmpty ¶
func (rm RecentMessage) IsEmpty() bool
type ReqCreateGroup ¶
type ReqCreateGroup struct {
	// Group names are limited to 25 characters. A longer group name will cause a 406 not acceptable error.
	Name string
	// You don't need to include your own JID in the participants array, the WhatsApp servers will add it implicitly.
	Participants []types.JID
	// A create key can be provided to deduplicate the group create notification that will be triggered
	// when the group is created. If provided, the JoinedGroup event will contain the same key.
	CreateKey types.MessageID
	// Set IsParent to true to create a community instead of a normal group.
	// When creating a community, the linked announcement group will be created automatically by the server.
	types.GroupParent
	// Set LinkedParentJID to create a group inside a community.
	types.GroupLinkedParent
}
ReqCreateGroup contains the request data for CreateGroup.

type SendRequestExtra ¶
type SendRequestExtra struct {
	// The message ID to use when sending. If this is not provided, a random message ID will be generated
	ID types.MessageID
	// JID of the bot to be invoked (optional)
	InlineBotJID types.JID
	// Should the message be sent as a peer message (protocol messages to your own devices, e.g. app state key requests)
	Peer bool
	// A timeout for the send request. Unlike timeouts using the context parameter, this only applies
	// to the actual response waiting and not preparing/encrypting the message.
	// Defaults to 75 seconds. The timeout can be disabled by using a negative value.
	Timeout time.Duration
	// When sending media to newsletters, the Handle field returned by the file upload.
	MediaHandle string
}
SendRequestExtra contains the optional parameters for SendMessage.

By default, optional parameters don't have to be provided at all, e.g.

cli.SendMessage(ctx, to, message)
When providing optional parameters, add a single instance of this struct as the last parameter:

cli.SendMessage(ctx, to, message, whatsmeow.SendRequestExtra{...})
Trying to add multiple extra parameters will return an error.

type SendResponse ¶
type SendResponse struct {
	// The message timestamp returned by the server
	Timestamp time.Time

	// The ID of the sent message
	ID types.MessageID

	// The server-specified ID of the sent message. Only present for newsletter messages.
	ServerID types.MessageServerID

	// Message handling duration, used for debugging
	DebugTimings MessageDebugTimings
}
type SetProxyOptions ¶
type SetProxyOptions struct {
	// If NoWebsocket is true, the proxy won't be used for the websocket
	NoWebsocket bool
	// If NoMedia is true, the proxy won't be used for media uploads/downloads
	NoMedia bool
}
type UploadResponse ¶
type UploadResponse struct {
	URL        string `json:"url"`
	DirectPath string `json:"direct_path"`
	Handle     string `json:"handle"`
	ObjectID   string `json:"object_id"`

	MediaKey      []byte `json:"-"`
	FileEncSHA256 []byte `json:"-"`
	FileSHA256    []byte `json:"-"`
	FileLength    uint64 `json:"-"`
}
UploadResponse contains the data from the attachment upload, which can be put into a message to send the attachment.

type UsyncQueryExtras ¶
type UsyncQueryExtras struct {
	BotListInfo []types.BotListInfo
}
type WebPushConfig ¶
type WebPushConfig struct {
	Endpoint string `json:"endpoint"`
	Auth     []byte `json:"auth"`
	P256DH   []byte `json:"p256dh"`
}
func (*WebPushConfig) GetPushConfigAttrs ¶
func (wpc *WebPushConfig) GetPushConfigAttrs() waBinary.Attrs