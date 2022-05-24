demo:
	LOCAL=demo go run . -f .env.demo env | sort
demo-convert:
	LOCAL=demo2 go run . -f .env.demo env | sort | sed -E 's/(.*)/-e \1/' | tr '\n' ' '
demo-override:
	LOCAL=demo2 go run . -o -f .env.demo env | grep PATH