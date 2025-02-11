package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

var (
    bot       *tgbotapi.BotAPI
    homeChatID int64
    input     *widget.Entry
    output    *widget.RichText
    caption   *widget.Entry
    sendButton *widget.Button
)

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

    c := make(chan os.Signal , 1)
    signal.Notify(c, os.Interrupt , syscall.SIGTERM)
    go func() {
        <-c 
        log.Printf("Exiting...")
        os.Exit(0)
        }()

    go listenForTelegramUpdates()

    // handleUserInput()

    // Initialize Fyne app
    a := app.New()
    w := a.NewWindow("Telegram Bot GUI")

    // // Create GUI components
    // input = widget.NewMultiLineEntry()
    // input.SetPlaceHolder("Type a message...")
    // input.Resize(fyne.NewSize(400, 60))

    output = widget.NewRichTextWithText("")
    output.Resize(fyne.NewSize(400, 300))

  

    input = widget.NewMultiLineEntry()  // Assign to global variable
    input.SetPlaceHolder("Type a message...")
    input.Resize(fyne.NewSize(400, 60))
    

    sendButton := widget.NewButton("Send", func() {
        text := input.Text
        if homeChatID == 0 {
            chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
            if chatIDStr != "" {
                var err error
                homeChatID, err = strconv.ParseInt(chatIDStr, 10, 64)
                if err != nil {
                    log.Printf("Error parsing TELEGRAM_CHAT_ID: %v", err)
                }
            }
        }
        if text != "" {
            sendMessage(text, homeChatID)
            output.AppendMarkdown(fmt.Sprintf("**You:** %s\n", text))
            input.SetText("")
        }
    })

    messageContainer := container.NewBorder(
        nil, nil, nil, sendButton,  // top, bottom, left, right
        input,
    )

    caption := widget.NewEntry()
    caption.SetPlaceHolder("Image caption (optional)")

    

    // New image button
    imageButton := widget.NewButton("Send Image", func() {
        handleImageSend(caption.Text)
        caption.SetText("")
    })

        imageContainer := container.NewBorder(
        nil, nil, nil, imageButton,
        caption,
    )
    // Main content layout
    content := container.NewVBox(
        // Chat history section
        widget.NewCard(
            "Chat History",  // title
            "",             // subtitle
            container.NewPadded(output),
        ),
        
        // Inputs section
        container.NewVBox(
            // Message area
            container.NewPadded(messageContainer),
            
            // Image area
            container.NewPadded(imageContainer),
        ),
    )

    // Add padding to the whole window
    paddedContent := container.NewPadded(content)


    w.SetContent(paddedContent)
    w.Resize(fyne.NewSize(800, 600)) 
    w.ShowAndRun()
}

func listenForTelegramUpdates() {
    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60

    updates := bot.GetUpdatesChan(u)

    for update := range updates {
        if update.Message == nil {
            continue
        }

        // Set homeChatID on first message
        if homeChatID == 0 {
            homeChatID = update.Message.Chat.ID
            log.Printf("Home Chat ID set to %d", homeChatID)
            sendMessage("Bot is now connected! Send me messages!", update.Message.Chat.ID)
        }

        // Handle incoming photos
        if len(update.Message.Photo) > 0 {
            handleReceivedImage(update.Message)
            output.AppendMarkdown(fmt.Sprintf("**Received Image** from %s\n", update.Message.From.UserName))
            continue
        }

        // Print received message to console
        if update.Message.Text != "" {
            fmt.Printf("\nHome Account: %s\n> ", update.Message.Text)
            output.AppendMarkdown(fmt.Sprintf("**%s:** %s\n", update.Message.From.UserName, update.Message.Text))
        }
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



func handleImageSend(caption string) {
    imgPath, err := getClipboardImage()
    if err != nil {
        log.Printf("Error getting image: %v", err)
        return
    }
    defer os.Remove(imgPath)

    // Check if file exists
    if _, err := os.Stat(imgPath); os.IsNotExist(err) {
        log.Printf("Image file not found: %v", err)
        return
    }

    // Send photo with caption
    photo := tgbotapi.NewPhoto(homeChatID, tgbotapi.FilePath(imgPath))
    if caption != "" {
    photo.Caption = caption
    }
    _, err = bot.Send(photo)
    if err != nil {
        log.Printf("Error sending photo: %v", err)
    } else {
        log.Println("Image sent successfully")
    }
}


func handleUserInput() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		text := scanner.Text()
		
		if text == "/image" {
            fmt.Print("Enter caption: (Enter to skip) ")
            scanner.Scan()
            caption := scanner.Text()
            
			handleImageSend(caption)
		} else if text != "" {
			sendMessage(text, homeChatID)
		}
		
		fmt.Print("> ")
	}
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

func sendMessage(text string, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := bot.Send(msg)
	if err != nil {
		log.Printf("Error sending message: %v", err)
	}
}


