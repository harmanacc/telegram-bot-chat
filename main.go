package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

var (
	bot         *tgbotapi.BotAPI
	homeChatID  int64
	mainWindow  fyne.Window
	statusLabel *widget.Label
	messageList *widget.List
)

var messages = binding.NewStringList()

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN must be set in .env")
	}

	bot, err = tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	// Setup GUI
	a := app.New()
	mainWindow = a.NewWindow("Telegram File Sender")
	mainWindow.Resize(fyne.NewSize(600, 400))

	setupUI()
	go listenForTelegramUpdates()

	// Handle OS signals
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Printf("Exiting...")
		os.Exit(0)
	}()

	mainWindow.ShowAndRun()
}

func setupUI() {
	// Message input
	messageEntry := widget.NewEntry()
	messageEntry.SetPlaceHolder("Type message here...")

	// Buttons
	sendTextBtn := widget.NewButton("Send Text", func() {
		sendMessage(messageEntry.Text, homeChatID)
		messageEntry.SetText("")
	})

	clipboardBtn := widget.NewButton("Send Clipboard Image", func() {
		go func() {
			caption := getCaptionFromDialog()
			handleImageSend(caption)
		}()
	})

	fileBtn := widget.NewButton("Select File", func() {
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err == nil && reader != nil {
				go handleFileSend(reader.URI().Path(), "")
			}
		}, mainWindow)
	})

	// Status label
	statusLabel = widget.NewLabel("Status: Ready")

	// Message history
	messageList = widget.NewListWithData(
		messages,
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(item binding.DataItem, obj fyne.CanvasObject) {
			str, err := item.(binding.String).Get()
			if err != nil {
				log.Printf("Error getting string: %v", err)
				return
			}
			obj.(*widget.Label).SetText(str)
		},
	)

	// Layout
	buttons := container.NewHBox(sendTextBtn, clipboardBtn, fileBtn)
	inputArea := container.NewVBox(messageEntry, buttons)
	mainWindow.SetContent(container.NewBorder(inputArea, statusLabel, nil, nil, messageList))
}

func getCaptionFromDialog() string {
	caption := ""
	entry := widget.NewEntry()
	dialog.ShowCustomConfirm(
		"Enter Caption",
		"OK",
		"Cancel",
		entry,
		func(ok bool) {
			if ok {
				caption = entry.Text
			}
		},
		mainWindow,
	)
	return caption
}

func updateStatus(text string) {
	statusLabel.SetText("Status: " + text)
	statusLabel.Refresh() // Trigger UI update
}

// Update addMessageToHistory:
func addMessageToHistory(text string) {
	messages.Append(text)
}

func listenForTelegramUpdates() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		if homeChatID == 0 {
			homeChatID = update.Message.Chat.ID
			updateStatus("Connected to Telegram!")
			sendMessage("Bot is now connected!", homeChatID)
		}

		if len(update.Message.Photo) > 0 {
			handleReceivedImage(update.Message)
			continue
		}

		if update.Message.Text != "" {
			addMessageToHistory(fmt.Sprintf("Received: %s", update.Message.Text))
		}
	}
}

func sendMessage(text string, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := bot.Send(msg)
	if err != nil {
		log.Printf("Error sending message: %v", err)
	}
}

func handleReceivedImage(message *tgbotapi.Message) {
	// Get the largest available photo size
	photo := message.Photo[len(message.Photo)-1]

	// Get file information from Telegram
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: photo.FileID})
	if err != nil {
		log.Printf("Error getting file: %v", err)
		return
	}

	// Download the image
	imageURL := file.Link(bot.Token)
	resp, err := http.Get(imageURL)
	if err != nil {
		log.Printf("Error downloading image: %v", err)
		return
	}
	defer resp.Body.Close()

	// Save the image locally
	imgPath := fmt.Sprintf("received_image_%d.jpg", message.MessageID)
	outFile, err := os.Create(imgPath)
	if err != nil {
		log.Printf("Error creating file: %v", err)
		return
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		log.Printf("Error saving image: %v", err)
		return
	}

	fmt.Printf("\nReceived image saved as: %s\n> ", imgPath)
}

func handleFileSend(filePath string, caption string) {
	updateStatus("Sending file...")
	defer updateStatus("Ready")

	file, err := os.Open(filePath)
	if err != nil {
		updateStatus("Error opening file")
		return
	}
	defer file.Close()

	// Check if it's an image
	if isImageFile(filePath) {
		photo := tgbotapi.NewPhoto(homeChatID, tgbotapi.FilePath(filePath))
		if caption != "" {
			photo.Caption = caption
		}
		_, err = bot.Send(photo)
	} else {
		doc := tgbotapi.NewDocument(homeChatID, tgbotapi.FilePath(filePath))
		if caption != "" {
			doc.Caption = caption
		}
		_, err = bot.Send(doc)
	}

	if err != nil {
		updateStatus(fmt.Sprintf("Error: %v", err))
		return
	}

	addMessageToHistory(fmt.Sprintf("Sent file: %s", filePath))
	updateStatus("File sent successfully")
}

func isImageFile(filePath string) bool {
	imageExtensions := []string{".jpg", ".jpeg", ".png", ".gif"}
	for _, ext := range imageExtensions {
		if strings.HasSuffix(strings.ToLower(filePath), ext) {
			return true
		}
	}
	return false
}

func getClipboardImage() (string, error) {
	// Check if clipboard contains an image
	cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("xclip error: %v", err)
	}

	if !containsImage(output) {
		return "", fmt.Errorf("no image in clipboard")
	}

	// Create temporary file
	tmpfile, err := os.CreateTemp("", "clipboard-*.png")
	if err != nil {
		return "", fmt.Errorf("temp file error: %v", err)
	}
	tmpfile.Close()

	// Save clipboard image to temp file
	cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	outFile, err := os.Create(tmpfile.Name())
	if err != nil {
		return "", fmt.Errorf("create file error: %v", err)
	}
	defer outFile.Close()

	cmd.Stdout = outFile
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("xclip save error: %v", err)
	}

	return tmpfile.Name(), nil
}

func containsImage(output []byte) bool {
	return bytes.Contains(output, []byte("image/png")) ||
		bytes.Contains(output, []byte("image/jpeg")) ||
		bytes.Contains(output, []byte("image/gif"))
}

func handleImageSend(caption string) {
	updateStatus("Processing clipboard image...")
	defer updateStatus("Ready")

	imgPath, err := getClipboardImage()
	if err != nil {
		updateStatus(fmt.Sprintf("Error: %v", err))
		return
	}
	defer os.Remove(imgPath)

	handleFileSend(imgPath, caption)
}

// The rest of the original functions (getClipboardImage, containsImage, handleReceivedImage, etc.)
// remain mostly unchanged except for replacing console output with GUI updates
