For starting bot you need 
1. Create telegram bot token from BotFather's and then run command
2. Running instance of Redis Database

Then run command
```shell script
go run main.go -token YOU_TG_BOT_TOKEN
```

Or you can build binary file and run
```shell script
go build -o cryptoangelbot main.go
chmod +x cryptoangelbot
./cryptoangelbot -token YOU_TG_BOT_TOKEN
```
