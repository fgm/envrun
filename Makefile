demo:
	LOCAL=demo go run . -f .env.demo env | sort
demo-convert:
	LOCAL=demo2 go run . -f .env.demo env | sort | sed -E 's/(.*)/-e \1/' | tr '\n' ' '