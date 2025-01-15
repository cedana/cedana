import argparse
import time

from transformers import AutoModelForCausalLM, AutoTokenizer

# Parse command-line arguments
parser = argparse.ArgumentParser(
    description='Configure sleep duration for the model script.'
)
parser.add_argument(
    '--sleep',
    type=int,
    default=0,
    help='Duration (in seconds) to sleep before starting the loop. Default is 10 seconds.',
)
args = parser.parse_args()

# Load the tokenizer and model
tokenizer = AutoTokenizer.from_pretrained('stabilityai/stablelm-2-1_6b')
model = AutoModelForCausalLM.from_pretrained(
    'stabilityai/stablelm-2-1_6b',
    torch_dtype='auto',
)
model.cuda()

# Sleep for the specified duration
time.sleep(args.sleep)

while True:
    user_input = 'some prompt'

    # Tokenize input
    inputs = tokenizer(user_input, return_tensors='pt').to(model.device)

    # Generate tokens
    tokens = model.generate(
        **inputs,
        max_new_tokens=64,
        temperature=0.70,
        top_p=0.95,
        do_sample=True,
    )

    output = tokenizer.decode(tokens[0], skip_special_tokens=True)
    print(f'Generated Output:\n{output}')
